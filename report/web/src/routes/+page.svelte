<script lang="ts">
	import { onMount, tick } from 'svelte';
	import type { Report, Species } from '../lib/types';
	import { addDays, day, hour, num, offset, pct, shortHour, stamp } from '../lib/format';
	import { CAST, colorAt } from '../lib/palette';
	import HourClock from '../lib/components/HourClock.svelte';
	import Heatmap from '../lib/components/Heatmap.svelte';
	import DailyBars from '../lib/components/DailyBars.svelte';
	import SpeciesCard from '../lib/components/SpeciesCard.svelte';
	import RecordingCard from '../lib/components/RecordingCard.svelte';
	import Ridgeline from '../lib/components/Ridgeline.svelte';
	import Stream from '../lib/components/Stream.svelte';
	import DawnPanels from '../lib/components/DawnPanels.svelte';
	import ChartTable from '../lib/components/ChartTable.svelte';
	import QualityPanel from '../lib/components/QualityPanel.svelte';
	import Growth from '../lib/components/Growth.svelte';
	import WeatherSong from '../lib/components/WeatherSong.svelte';
	import Soundscape from '../lib/components/Soundscape.svelte';
	import SpeciesDetail from '../lib/components/SpeciesDetail.svelte';
	import ChangeChip from '../lib/components/ChangeChip.svelte';
	import { player } from '../lib/player.svelte';

	let report = $state<Report | null>(null);
	let failed = $state(false);
	let printMode = $state(false);

	const top = $derived(report?.species[0]);
	const regulars = $derived(report ? report.species.slice(0, 9) : []);
	const heat = $derived(report ? report.species.slice(0, 14) : []);
	const newcomers = $derived(report ? report.species.filter((s) => s.isNew).slice(0, 8) : []);
	const nightShift = $derived(
		report ? report.species.filter((s) => s.count >= 10 && s.nightShare >= 0.6).slice(0, 4) : []
	);
	const earlyRisers = $derived(
		report
			? report.species.filter((s) => s.count >= 5 && s.peakHour >= 5 && s.peakHour <= 8).slice(0, 4)
			: []
	);
	const rarest = $derived(
		report
			? [...report.species]
					.filter((s) => s.rarityScore !== undefined && s.rarityScore > 0)
					.sort((a, b) => (a.rarityScore ?? 1) - (b.rarityScore ?? 1))
					.slice(0, 4)
			: []
	);
	const credits = $derived(
		report ? report.species.filter((s) => s.photo?.author).map((s) => ({ s, p: s.photo! })) : []
	);
	// The hour-of-day charts draw the most-heard species; one species keeps one
	// colour across all three charts.
	const cast = $derived(
		report ? report.species.slice(0, CAST).filter((s) => s.count > 0).map((s, i) => ({ s, color: colorAt(i) })) : []
	);
	const hasPatterns = $derived(cast.length >= 2);
	const colorOf = (code: string) => colorAt(report?.species.findIndex((s) => s.code === code) ?? -1);

	// Numerals follow the sections that are present.
	const ROMAN = ['I', 'II', 'III', 'IV', 'V', 'VI', 'VII', 'VIII', 'IX'];
	const nums = $derived.by(() => {
		const keys = ['brief', report?.recordings && 'listen', 'rhythm', hasPatterns && 'patterns', 'regulars', 'notable', 'days', 'list', report?.quality && 'quality'];
		return Object.fromEntries(keys.filter(Boolean).map((k, i) => [k, ROMAN[i]])) as Record<string, string>;
	});

	// Table twins of the charts.
	const hourHead = Array.from({ length: 24 }, (_, h) => shortHour(h));
	const share = (n: number, t: number) => (!n ? '·' : n / t < 0.005 ? '<1%' : pct(n / t));
	const shareRows = $derived(
		cast.map(({ s }) => {
			const t = s.hourly.reduce((a, b) => a + b, 0);
			return { name: s.common, cells: s.hourly.map((n) => share(n, t)) };
		})
	);
	const countRows = $derived(cast.map(({ s }) => ({ name: s.common, cells: s.hourly.map((n) => (n ? num(n) : '·')) })));
	const dawnHead = $derived(report?.dawn?.series[0]?.days.map((d) => day(d.date, { month: 'short', day: 'numeric' })) ?? []);
	const dawnRows = $derived(
		(report?.dawn?.series ?? []).map((s) => ({ name: s.common, cells: s.days.map((d) => (d.minutes === null ? '–' : offset(d.minutes))) }))
	);
	const growthRows = $derived(
		(report?.growth?.days ?? [])
			.filter((d) => d.new > 0)
			.map((d) => ({ name: day(d.date, { weekday: 'short', month: 'short', day: 'numeric' }), cells: [String(d.new), String(d.total), d.species?.join(', ') ?? ''] }))
	);
	const weatherRows = $derived.by(() => {
		const w = report?.weather;
		if (!w) return [];
		return Array.from({ length: w.hours.length / 24 }, (_, k) => {
			const hs = w.hours.slice(k * 24, (k + 1) * 24);
			const ts = hs.flatMap((h) => (h.t === undefined ? [] : [h.t]));
			return {
				name: day(addDays(w.start, k), { weekday: 'short', month: 'short', day: 'numeric' }),
				cells: [num(hs.reduce((a, h) => a + h.n, 0)), ts.length ? `${Math.min(...ts).toFixed(0)} to ${Math.max(...ts).toFixed(0)} °C` : '–', String(hs.filter((h) => h.r).length)]
			};
		});
	});
	const soundRows = $derived((report?.soundscape?.bands ?? []).map((b, i) => ({ name: b, cells: report!.soundscape!.levels[i].map((v) => (v === null ? '·' : v.toFixed(1))) })));
	const photoOf = (sci: string) => report?.species.find((s) => s.scientific === sci)?.photo;
	const pdfName = $derived(report ? `field-notes-${report.period.end}.pdf` : 'field-notes.pdf');

	// How fresh the page is. It is rebuilt every hour, so say when, and keep the
	// "ago" ticking for anyone who leaves the tab open.
	let now = $state(Date.now());
	$effect(() => {
		const t = setInterval(() => (now = Date.now()), 30_000);
		return () => clearInterval(t);
	});
	const age = $derived(report ? Math.floor((now - Date.parse(report.generatedAt)) / 60_000) : 0);
	const ago = $derived(
		age < 1
			? 'just now'
			: age < 60
				? `${age} min ago`
				: age < 1440
					? `${Math.floor(age / 60)} h ago`
					: `${Math.floor(age / 1440)} d ago`
	);

	onMount(async () => {
		printMode = new URLSearchParams(location.search).has('print');
		try {
			const r = await fetch('data/report.json', { cache: 'no-cache' });
			if (!r.ok) throw new Error(String(r.status));
			report = await r.json();
			document.title = `${report!.title} · ${report!.period.label} · updated ${stamp(report!.generatedAt)}`;
			const code = new URLSearchParams(location.search).get('species');
			if (code && !printMode) selected = report!.species.find((s) => s.code === code) ?? null;
		} catch {
			failed = true;
		}
		await tick();
		// Tell the PDF printer (and anyone waiting) that the page is complete.
		// Lazy images below the fold never load on their own; print mode makes
		// every image eager, so the PDF waits for all of them.
		await Promise.all(
			Array.from(document.images)
				.filter((img) => !img.complete && img.loading !== 'lazy')
				.map((img) => img.decode().catch(() => null))
		);
		document.documentElement.dataset.ready = '1';
	});

	// A species' own view, opened from a card or a link (?species=code).
	let selected = $state<Species | null>(null);
	function openSpecies(s: Species) {
		selected = s;
		const u = new URL(location.href);
		u.searchParams.set('species', s.code);
		history.replaceState(null, '', u);
	}
	function closeSpecies() {
		selected = null;
		const u = new URL(location.href);
		u.searchParams.delete('species');
		history.replaceState(null, '', u);
	}

	// "Play all": the best recordings, then the latest.
	const queue = $derived(report?.recordings ? [...report.recordings.best, ...report.recordings.recent] : []);

	const onPrint = () => window.print();
	const lead = (s: Species) => `${num(s.count)} detections · peak at ${hour(s.peakHour)}`;
</script>

{#if failed}
	<main class="empty">
		<h1>Field Notes</h1>
		<p>The first report is being prepared. Check back soon.</p>
	</main>
{:else if !report}
	<main class="empty"><p class="loading">Opening the notebook…</p></main>
{:else}
	<nav class="actions no-print">
		{#if report.pdf}<a class="btn" href={report.pdf} download={pdfName}>Download PDF</a>{/if}
		<button class="btn ghost" onclick={onPrint}>Print</button>
	</nav>

	<!-- Cover -->
	<header class="cover">
		{#if top?.photo}
			<img class="hero" src={top.photo.large} alt={top.common} loading="eager" />
		{/if}
		<div class="veil"></div>
		<p class="updated" title="Rebuilt every hour">
			<span class="pulse" class:stale={age >= 180} aria-hidden="true"></span>
			Updated <time datetime={report.generatedAt}>{stamp(report.generatedAt)}</time>
			<span class="ago no-print">· {ago}</span>
		</p>
		<div class="masthead">
			<p class="kicker">{report.station}</p>
			<h1>{report.title}</h1>
			<p class="subtitle">{report.subtitle}</p>
			<p class="period">{report.period.label}</p>
		</div>
		{#if top}
			<p class="hero-caption">
				<em>On the cover:</em> {top.common}, the most-heard bird this period{#if top.photo?.author}. Photo: {top.photo.author}{/if}
			</p>
		{/if}
	</header>

	<main>
		<section class="stats">
			<div class="stat"><span class="v">{num(report.totals.detections)}</span><span class="l">detections</span></div>
			<div class="stat"><span class="v">{report.totals.species}</span><span class="l">species</span></div>
			<div class="stat"><span class="v">{report.totals.newSpecies}</span><span class="l">new to the station</span></div>
			<div class="stat"><span class="v">{hour(report.totals.busiestHour)}</span><span class="l">busiest hour</span></div>
		</section>

		<section class="brief">
			<h2><span class="num">{nums.brief}</span> The period in brief</h2>
			<ol>
				{#each report.highlights as h, i (i)}
					<li>{h}</li>
				{/each}
			</ol>
		</section>

		{#if report.recordings}
			<section class="listen">
				<h2><span class="num">{nums.listen}</span> Listen</h2>
				<p class="dek">
					Recordings straight from the station: the clearest calls of the period, and the latest birds it heard.
					Tap a spectrogram to play from that point.
				</p>
				<div class="playall no-print">
					{#if player.queue.length}
						<button class="btn small" onclick={() => player.stop()}>■ Stop</button>
						<span>Playing {player.index + 1} of {player.queue.length}: {player.current?.common}</span>
					{:else if queue.length > 1}
						<button class="btn small" onclick={() => player.playAll(queue)}>▶ Play all {queue.length}</button>
					{/if}
				</div>
				{#if report.recordings.best.length}
					<h3>Best recordings</h3>
					<div class="recs">
						{#each report.recordings.best as r (r.audio)}
							<RecordingCard {r} photo={photoOf(r.scientific)} eager={printMode} />
						{/each}
					</div>
				{/if}
				{#if report.recordings.recent.length}
					<h3>Just heard</h3>
					<div class="recs">
						{#each report.recordings.recent as r (r.audio)}
							<RecordingCard {r} photo={photoOf(r.scientific)} eager={printMode} />
						{/each}
					</div>
				{/if}
				<p class="print-only listen-note">The recordings play on the web edition of these notes.</p>
			</section>
		{/if}

		<section class="rhythm">
			<h2><span class="num">{nums.rhythm}</span> Twenty-four hours</h2>
			<p class="dek">
				When the station heard birds, hour by hour across {report.period.days} days. Indigo marks the
				hours between 7 pm and 6 am, when {pct(report.totals.nightShare)} of all detections happened.
			</p>
			<div class="rhythm-grid">
				<div class="clock-wrap"><HourClock values={report.hourly} total={report.totals.detections} /></div>
				<div class="heat-wrap">
					<h3>Hour by hour</h3>
					<Heatmap species={heat} />
				</div>
			</div>
		</section>

		{#if hasPatterns}
			<section class="patterns">
				<h2><span class="num">{nums.patterns}</span> The daily handoff</h2>
				<p class="dek">
					Who sings when, how the most-heard species take turns through the day, and how early the morning
					singers start.
				</p>
				<div class="charts">
					<div class="chart">
						<h3>Who sings when</h3>
						<p class="dek small">
							Each ridge is one species' day, as a share of its own detections. A tall, narrow ridge is a bird
							with its hour; a low, wide one calls around the clock.
						</p>
						<Ridgeline items={cast} />
						<ChartTable label="Show as a table" corner="Share of detections" head={hourHead} rows={shareRows} />
					</div>
					<div class="chart">
						<h3>Acoustic succession</h3>
						<p class="dek small">
							The {cast.length} most-heard species handing the day to each other. A band's thickness is its
							detections in that hour.
						</p>
						<Stream items={cast} />
						<ChartTable label="Show as a table" corner="Detections" head={hourHead} rows={countRows} />
					</div>
					{#if report.dawn}
						<div class="chart">
							<h3>Dawn chorus onset</h3>
							<p class="dek small">
								Minutes after civil dawn that each morning singer started, day by day; lower means earlier.
								Birds that call at night, and days the station was not yet listening at dawn, are left out.
							</p>
							<DawnPanels series={report.dawn.series} {colorOf} />
							<ChartTable label="Show as a table" corner="After civil dawn" head={dawnHead} rows={dawnRows} />
						</div>
					{/if}
					{#if report.weather}
						<div class="chart">
							<h3>Weather and song</h3>
							<p class="dek small">Detections an hour above the temperature, with the hours it rained shaded. {report.weather.insight}</p>
							<WeatherSong w={report.weather} />
							<ChartTable label="Show as a table" corner="Day" head={['Detections', 'Temperature', 'Rain hours']} rows={weatherRows} />
						</div>
					{/if}
					{#if report.soundscape}
						<div class="chart">
							<h3>The soundscape</h3>
							<p class="dek small">How loud the station was, hour by hour and pitch by pitch, with the birds heard above it. Low bands are traffic, wind and hum; the middle and high bands are where birds sing. {report.soundscape.insight}</p>
							<Soundscape s={report.soundscape} hourly={report.hourly} />
							<ChartTable label="Show as a table" corner="Level, dB" head={hourHead} rows={soundRows} />
						</div>
					{/if}
				</div>
			</section>
		{/if}

		<section class="regulars page-break">
			<h2><span class="num">{nums.regulars}</span> The regulars</h2>
			<p class="dek">The {regulars.length} most-heard species, with the hours they called.</p>
			<div class="cards">
				{#each regulars as s, i (s.code)}
					<SpeciesCard {s} rank={i + 1} eager={printMode || i < 3} onopen={openSpecies} compare={report.compare} />
				{/each}
			</div>
		</section>

		<section class="notable page-break">
			<h2><span class="num">{nums.notable}</span> Worth noting</h2>
			<div class="notes">
				{#if newcomers.length}
					<div class="note">
						<h3>New arrivals</h3>
						<p class="dek small">First heard at the station during this period.</p>
						<ul class="mini">
							{#each newcomers as s (s.code)}
								<li>
									{#if s.photo}<img src={s.photo.small} alt="" loading={printMode ? 'eager' : 'lazy'} />{/if}
									<div><strong>{s.common}</strong><span>first heard {stamp(s.firstHeard)}</span></div>
								</li>
							{/each}
						</ul>
					</div>
				{/if}
				{#if nightShift.length}
					<div class="note">
						<h3>The night shift</h3>
						<p class="dek small">Mostly heard between 7 pm and 6 am.</p>
						<ul class="mini">
							{#each nightShift as s (s.code)}
								<li>
									{#if s.photo}<img src={s.photo.small} alt="" loading={printMode ? 'eager' : 'lazy'} />{/if}
									<div><strong>{s.common}</strong><span>{pct(s.nightShare)} at night · {lead(s)}</span></div>
								</li>
							{/each}
						</ul>
					</div>
				{/if}
				{#if earlyRisers.length}
					<div class="note">
						<h3>Early risers</h3>
						<p class="dek small">Loudest in the first hours of the morning.</p>
						<ul class="mini">
							{#each earlyRisers as s (s.code)}
								<li>
									{#if s.photo}<img src={s.photo.small} alt="" loading={printMode ? 'eager' : 'lazy'} />{/if}
									<div><strong>{s.common}</strong><span>{lead(s)}</span></div>
								</li>
							{/each}
						</ul>
					</div>
				{/if}
				{#if rarest.length}
					<div class="note">
						<h3>Least expected</h3>
						<p class="dek small">Lowest likelihood for this area and season.</p>
						<ul class="mini">
							{#each rarest as s (s.code)}
								<li>
									{#if s.photo}<img src={s.photo.small} alt="" loading={printMode ? 'eager' : 'lazy'} />{/if}
									<div><strong>{s.common}</strong><span>{s.rarity} · {num(s.count)} detections</span></div>
								</li>
							{/each}
						</ul>
					</div>
				{/if}
					{#if report.compare?.quiet?.length}
						<div class="note">
							<h3>Gone quiet</h3>
							<p class="dek small">Regulars heard far less than {report.compare.label}, per listening day.</p>
							<ul class="mini">
								{#each report.compare.quiet as q (q.code)}
									{@const sp = report.species.find((x) => x.code === q.code)}
									<li>
										{#if sp?.photo}<img src={sp.photo.small} alt="" loading={printMode ? 'eager' : 'lazy'} />{/if}
										<div><strong>{q.common}</strong><span>{num(q.prev)} before, {num(q.now)} now · down {Math.round(-q.change * 100)}%</span></div>
									</li>
								{/each}
							</ul>
						</div>
					{/if}
					{#if report.compare?.notHeard?.length}
						<div class="note">
							<h3>Not heard this time</h3>
							<p class="dek small">Heard {report.compare.label}, silent now.</p>
							<ul class="mini">
								{#each report.compare.notHeard as m (m.code)}
									<li><div><strong>{m.common}</strong><span>{num(m.count)} detections before</span></div></li>
								{/each}
							</ul>
						</div>
					{/if}
			</div>
		</section>

		<section class="days-sec">
			<h2><span class="num">{nums.days}</span> Day by day</h2>
			<p class="dek">
				Detections per day and how many species made them. The busiest day was
				{day(report.totals.busiestDay, { weekday: 'long', month: 'long', day: 'numeric' })}.
			</p>
			<DailyBars days={report.daily} />
			{#if report.growth}
				<div class="chart growth">
					<h3>Species so far</h3>
					<p class="dek small">How many different species the station has heard in all, by day. Each step up is a species heard for the first time; it stands at {report.growth.total}.</p>
					<Growth g={report.growth} />
					<ChartTable label="Show as a table" corner="Day" head={['New', 'Total', 'First heard']} rows={growthRows} />
				</div>
			{/if}
		</section>

		<section class="list">
			<h2><span class="num">{nums.list}</span> Every species</h2>
			<table>
				<thead>
					<tr>
						<th></th>
						<th>Species</th>
						<th class="r">Detections</th>
						<th class="r">Days</th>
						<th>Peak</th>
						<th>First heard</th>
						<th>Likelihood</th>
							{#if report.compare}<th>vs {report.compare.label}</th>{/if}
					</tr>
				</thead>
				<tbody>
					{#each report.species as s (s.code)}
						<tr>
							<td class="thumb">
								{#if s.photo}<img src={s.photo.small} alt="" loading={printMode ? 'eager' : 'lazy'} />{/if}
							</td>
							<td>
								<button class="link" onclick={() => openSpecies(s)}><strong>{s.common}</strong></button>{#if s.isNew}<span class="dot" title="new"></span>{/if}
								<em>{s.scientific}</em>
							</td>
							<td class="r">{num(s.count)}</td>
							<td class="r">{s.daysHeard}</td>
							<td>{hour(s.peakHour)}</td>
							<td>{stamp(s.firstHeard)}</td>
							<td class="cap">{s.rarity ?? ''}</td>
								{#if report.compare}<td><ChangeChip change={s.change} prev={s.prev ?? 0} label={report.compare.label} /></td>{/if}
						</tr>
					{/each}
				</tbody>
			</table>
		</section>

		{#if report.quality}
			<section class="quality">
				<h2><span class="num">{nums.quality}</span> How sure we are</h2>
				<p class="dek">
					BirdNET-Go's models sometimes mistake noise for birds. Two models hearing the same sound and agreeing is the best
					evidence there is, and every detection a person marks as a false positive is left out of this report.
				</p>
				<QualityPanel q={report.quality} />
			</section>
		{/if}

		<footer class="colophon">
			<h2>Colophon</h2>
			<p>
				Birds identified by <a href="https://github.com/tphakala/birdnet-go" rel="noreferrer">BirdNET-Go</a>
				running BirdNET and Perch models on a home station; only detections that passed its filters are
				counted. Night is a fixed 7 pm–6 am window. Generated {stamp(report.generatedAt)} for
				{report.period.label}.
			</p>
			{#if credits.length}
				<h3>Photographs</h3>
				<p class="credits">
					{#each credits as { s, p }, i (s.code)}
						<span>
							{s.common}: {p.author}{#if p.license}, {#if p.licenseUrl}<a href={p.licenseUrl} rel="noreferrer">{p.license}</a>{:else}{p.license}{/if}{/if}{#if i < credits.length - 1}<span class="sep"> · </span>{/if}
						</span>
					{/each}
					<br /><small>Photos via Avicommons and BirdNET-Go.</small>
				</p>
			{/if}
		</footer>
	</main>

	<SpeciesDetail s={selected} {report} onclose={closeSpecies} />
{/if}

<style>
	.empty {
		min-height: 80vh;
		display: grid;
		place-content: center;
		text-align: center;
	}
	.empty h1 {
		font: italic 600 3rem var(--serif);
		margin: 0;
	}
	.loading {
		font: italic 400 1.2rem var(--serif);
		color: var(--muted);
	}

	.actions {
		position: fixed;
		right: 1rem;
		top: 1rem;
		z-index: 10;
		display: flex;
		gap: 0.5rem;
	}
	.btn {
		font: 600 0.8rem var(--sans);
		letter-spacing: 0.04em;
		border: 0;
		border-radius: 999px;
		padding: 0.6rem 1rem;
		background: var(--forest);
		color: #fbf8f1;
		text-decoration: none;
		cursor: pointer;
		box-shadow: 0 6px 18px -8px rgba(31, 42, 36, 0.6);
	}
	.btn.ghost {
		background: rgba(251, 248, 241, 0.92);
		color: var(--ink);
	}

	/* Cover */
	.cover {
		position: relative;
		min-height: min(88vh, 760px);
		display: flex;
		align-items: flex-end;
		overflow: hidden;
		background: var(--night);
	}
	.hero {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		object-fit: cover;
		object-position: center 22%;
	}
	.veil {
		position: absolute;
		inset: 0;
		background: linear-gradient(
			180deg,
			rgba(20, 26, 22, 0) 30%,
			rgba(20, 26, 22, 0.55) 62%,
			rgba(20, 26, 22, 0.88) 100%
		);
	}
	.masthead {
		position: relative;
		padding: 0 clamp(1.25rem, 6vw, 5rem) clamp(3rem, 8vh, 5rem);
		color: #f8f3e8;
		max-width: 60rem;
	}
	.kicker {
		font: 600 0.75rem var(--sans);
		letter-spacing: 0.22em;
		text-transform: uppercase;
		opacity: 0.85;
		margin: 0 0 0.75rem;
	}
	h1 {
		font: italic 600 clamp(3.4rem, 10vw, 7.5rem) / 0.92 var(--serif);
		margin: 0;
		letter-spacing: -0.02em;
		font-variation-settings:
			'SOFT' 100,
			'WONK' 1;
	}
	.subtitle {
		font: 400 clamp(1.1rem, 2.2vw, 1.5rem) / 1.35 var(--serif);
		margin: 1rem 0 0.5rem;
		max-width: 36rem;
		opacity: 0.95;
	}
	.period {
		font: 600 0.85rem var(--sans);
		letter-spacing: 0.12em;
		text-transform: uppercase;
		margin: 0;
		color: var(--gold);
	}
	.updated {
		position: absolute;
		left: clamp(1.25rem, 6vw, 5rem);
		top: 1.1rem;
		margin: 0;
		display: flex;
		align-items: center;
		gap: 0.4rem;
		font: 500 0.74rem var(--sans);
		letter-spacing: 0.02em;
		color: rgba(248, 243, 232, 0.96);
		background: rgba(20, 26, 22, 0.45);
		padding: 0.38rem 0.8rem;
		border-radius: 999px;
		backdrop-filter: blur(4px);
	}
	.updated .ago {
		opacity: 0.8;
	}
	.pulse {
		width: 0.5rem;
		height: 0.5rem;
		border-radius: 50%;
		background: #86d596;
		box-shadow: 0 0 0 3px rgba(134, 213, 150, 0.25);
		margin-right: 0.15rem;
	}
	.pulse.stale {
		background: var(--gold);
		box-shadow: 0 0 0 3px rgba(212, 162, 76, 0.28);
	}
	/* On a phone the Download and Print buttons take the top-right corner. */
	@media (max-width: 760px) {
		.updated {
			top: 4.1rem;
		}
	}
	.hero-caption {
		position: absolute;
		right: 1.2rem;
		top: 4.4rem;
		margin: 0;
		font: 400 0.72rem var(--sans);
		color: rgba(248, 243, 232, 0.92);
		max-width: 22rem;
		text-align: right;
		background: rgba(20, 26, 22, 0.42);
		padding: 0.35rem 0.7rem;
		border-radius: 10px;
		backdrop-filter: blur(4px);
	}

	main {
		max-width: 72rem;
		margin: 0 auto;
		padding: 0 clamp(1.25rem, 4vw, 2.5rem) 4rem;
	}
	section {
		margin-top: clamp(3rem, 7vw, 5rem);
	}
	h2 {
		font: 600 clamp(1.8rem, 3.6vw, 2.6rem) / 1.1 var(--serif);
		margin: 0 0 0.6rem;
		letter-spacing: -0.01em;
		display: flex;
		align-items: baseline;
		gap: 0.8rem;
	}
	h2 .num {
		font: italic 500 0.55em var(--serif);
		color: var(--rust);
		min-width: 1.6em;
	}
	h3 {
		font: 600 1.15rem var(--serif);
		margin: 0 0 0.4rem;
	}
	.dek {
		color: var(--muted);
		max-width: 44rem;
		margin: 0 0 1.75rem;
		font: 400 1.05rem/1.55 var(--serif);
	}
	.dek.small {
		font-size: 0.9rem;
		margin-bottom: 0.9rem;
	}

	/* Stats strip, overlapping the cover */
	.stats {
		margin-top: -2.5rem;
		position: relative;
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		background: var(--card);
		border: 1px solid var(--rule);
		border-radius: 20px;
		box-shadow: 0 18px 40px -24px rgba(31, 42, 36, 0.5);
	}
	.stat {
		padding: 1.3rem 1.4rem;
		display: grid;
		gap: 0.2rem;
		border-left: 1px solid var(--rule);
	}
	.stat:first-child {
		border-left: 0;
	}
	.stat .v {
		font: 600 clamp(1.7rem, 3.4vw, 2.5rem) / 1 var(--serif);
		color: var(--forest);
	}
	.stat .l {
		font: 600 0.68rem var(--sans);
		letter-spacing: 0.12em;
		text-transform: uppercase;
		color: var(--muted);
	}

	/* Brief */
	.brief ol {
		list-style: none;
		counter-reset: b;
		padding: 0;
		margin: 1.2rem 0 0;
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(19rem, 1fr));
		gap: 1rem 2rem;
	}
	.brief li {
		counter-increment: b;
		font: 400 1.12rem/1.5 var(--serif);
		padding-left: 2.6rem;
		position: relative;
		break-inside: avoid;
	}
	.brief li::before {
		content: counter(b);
		position: absolute;
		left: 0;
		top: -0.05rem;
		font: italic 600 1.7rem/1 var(--serif);
		color: var(--gold);
	}

	/* Rhythm */
	.rhythm-grid {
		display: grid;
		grid-template-columns: minmax(16rem, 22rem) 1fr;
		gap: 2.5rem;
		align-items: start;
	}
	.heat-wrap,
	.clock-wrap {
		background: var(--card);
		border: 1px solid var(--rule);
		border-radius: 20px;
		padding: 1.4rem;
		break-inside: avoid;
	}

	/* Daily handoff: three chart cards */
	.charts {
		display: grid;
		gap: 1.3rem;
	}
	.chart {
		background: var(--card);
		border: 1px solid var(--rule);
		border-radius: 20px;
		padding: 1.4rem 1.4rem 1.2rem;
		break-inside: avoid;
	}
	.chart h3 {
		margin: 0 0 0.2rem;
	}
	.chart .dek.small {
		max-width: 40rem;
	}

	/* Recordings */
	.listen h3 {
		margin: 1.4rem 0 0.8rem;
	}
	.recs {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(17rem, 1fr));
		gap: 1.3rem;
	}
	.listen-note {
		font: italic 400 0.8rem var(--serif);
		color: var(--muted);
	}

	/* Cards */
	.cards {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(17rem, 1fr));
		gap: 1.3rem;
	}

	/* Notable */
	.notes {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(16rem, 1fr));
		gap: 1.3rem;
	}
	.note {
		background: var(--card);
		border: 1px solid var(--rule);
		border-radius: 20px;
		padding: 1.3rem 1.3rem 0.8rem;
		break-inside: avoid;
	}
	.mini {
		list-style: none;
		padding: 0;
		margin: 0;
	}
	.mini li {
		display: flex;
		gap: 0.75rem;
		align-items: center;
		padding: 0.55rem 0;
		border-top: 1px solid var(--rule);
	}
	.mini img {
		width: 3.2rem;
		height: 3.2rem;
		object-fit: cover;
		border-radius: 50%;
		flex: none;
	}
	.mini strong {
		display: block;
		font: 600 0.98rem var(--serif);
	}
	.mini span {
		font-size: 0.8rem;
		color: var(--muted);
	}

	/* Table */
	table {
		width: 100%;
		border-collapse: collapse;
		font-size: 0.88rem;
	}
	thead th {
		font: 600 0.64rem var(--sans);
		letter-spacing: 0.1em;
		text-transform: uppercase;
		color: var(--muted);
		text-align: left;
		padding: 0.5rem 0.6rem;
		border-bottom: 1.5px solid var(--ink);
	}
	td {
		padding: 0.45rem 0.6rem;
		border-bottom: 1px solid var(--rule);
		vertical-align: middle;
	}
	tr {
		break-inside: avoid;
	}
	td strong {
		font: 600 0.95rem var(--serif);
	}
	td em {
		display: block;
		font: italic 400 0.8rem var(--serif);
		color: var(--muted);
	}
	.r {
		text-align: right;
	}
	.cap {
		text-transform: capitalize;
		color: var(--muted);
	}
	.thumb {
		width: 2.6rem;
	}
	.thumb img {
		width: 2.2rem;
		height: 2.2rem;
		border-radius: 50%;
		object-fit: cover;
		display: block;
	}
	.dot {
		display: inline-block;
		width: 0.45rem;
		height: 0.45rem;
		border-radius: 50%;
		background: var(--rust);
		margin-left: 0.4rem;
		vertical-align: middle;
	}

	/* Colophon */
	.colophon {
		margin-top: 4rem;
		padding-top: 1.5rem;
		border-top: 1.5px solid var(--ink);
		color: var(--muted);
		font-size: 0.85rem;
	}
	.colophon h2 {
		font-size: 1.3rem;
	}
	.colophon h3 {
		font-size: 0.95rem;
		color: var(--ink);
		margin-top: 1rem;
	}
	.colophon a {
		color: var(--forest);
	}
	.credits {
		font-size: 0.78rem;
		line-height: 1.7;
	}

	@media (max-width: 760px) {
		.stats {
			grid-template-columns: repeat(2, 1fr);
		}
		.stat:nth-child(3) {
			border-left: 0;
		}
		.stat:nth-child(n + 3) {
			border-top: 1px solid var(--rule);
		}
		.rhythm-grid {
			grid-template-columns: 1fr;
		}
		th:nth-child(4),
		td:nth-child(4),
		th:nth-child(5),
		td:nth-child(5),
		th:nth-child(6),
		td:nth-child(6),
		th:nth-child(7),
		td:nth-child(7),
		th:nth-child(8),
		td:nth-child(8) {
			display: none;
		}
		.hero-caption {
			display: none;
		}
	}

	@media print {
		.cover {
			min-height: 0;
			height: 8.6in;
		}
		.stats {
			margin-top: 1.2rem;
			box-shadow: none;
		}
		section {
			margin-top: 2rem;
		}
		.rhythm-grid {
			grid-template-columns: 2.6in 1fr;
		}
		.regulars .dek {
			margin-bottom: 0.8rem;
		}
		.cards {
			grid-template-columns: repeat(3, 1fr);
			gap: 0.7rem;
		}
		.notes {
			grid-template-columns: repeat(2, 1fr);
		}
		.brief ol {
			grid-template-columns: 1fr 1fr;
		}
		.recs {
			grid-template-columns: repeat(3, 1fr);
			gap: 0.7rem;
		}
		.listen h3 {
			margin: 0.8rem 0 0.5rem;
		}
		/* Listen fills the rest of page 2; the clock and heatmap start page 3. */
		.listen + .rhythm {
			break-before: page;
		}
	}
	.btn.small {
		padding: 0.45rem 0.9rem;
		font-size: 0.74rem;
		box-shadow: none;
	}
	.playall {
		display: flex;
		align-items: center;
		gap: 0.8rem;
		margin: -0.6rem 0 1.4rem;
		font: 500 0.82rem var(--sans);
		color: var(--muted);
		min-height: 2.1rem;
	}
	.growth {
		margin-top: 1.6rem;
	}
	.quality h2 {
		margin-bottom: 0.6rem;
	}
	.link {
		border: 0;
		background: none;
		padding: 0;
		font: inherit;
		color: inherit;
		cursor: pointer;
		text-align: left;
	}
	.link:hover strong {
		text-decoration: underline;
		text-underline-offset: 3px;
	}
	@media print {
		.quality {
			break-inside: avoid;
		}
	}
</style>
