<script lang="ts">
	import { onMount, tick } from 'svelte';
	import type { Report, Species } from '../lib/types';
	import { day, hour, num, pct, stamp } from '../lib/format';
	import HourClock from '../lib/components/HourClock.svelte';
	import Heatmap from '../lib/components/Heatmap.svelte';
	import DailyBars from '../lib/components/DailyBars.svelte';
	import SpeciesCard from '../lib/components/SpeciesCard.svelte';
	import RecordingCard from '../lib/components/RecordingCard.svelte';

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
	// Sections after "Listen" move up a numeral when there are no recordings.
	const ROMAN = ['I', 'II', 'III', 'IV', 'V', 'VI', 'VII'];
	const numeral = (n: number) => ROMAN[n - 1 - (report?.recordings ? 0 : 1)];
	const photoOf = (sci: string) => report?.species.find((s) => s.scientific === sci)?.photo;
	const pdfName = $derived(report ? `field-notes-${report.period.end}.pdf` : 'field-notes.pdf');

	onMount(async () => {
		printMode = new URLSearchParams(location.search).has('print');
		try {
			const r = await fetch('data/report.json', { cache: 'no-cache' });
			if (!r.ok) throw new Error(String(r.status));
			report = await r.json();
			document.title = `${report!.title} · ${report!.period.label}`;
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
			<h2><span class="num">I</span> The period in brief</h2>
			<ol>
				{#each report.highlights as h, i (i)}
					<li>{h}</li>
				{/each}
			</ol>
		</section>

		{#if report.recordings}
			<section class="listen">
				<h2><span class="num">II</span> Listen</h2>
				<p class="dek">
					Recordings straight from the station: the clearest calls of the period, and the latest birds it heard.
					Tap a spectrogram to play from that point.
				</p>
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
			<h2><span class="num">{numeral(3)}</span> Twenty-four hours</h2>
			<p class="dek">
				When the station heard birds, hour by hour across {report.period.days} days. Indigo marks the
				hours between 7 pm and 6 am, when {pct(report.totals.nightShare)} of all detections happened.
			</p>
			<div class="rhythm-grid">
				<div class="clock-wrap"><HourClock values={report.hourly} total={report.totals.detections} /></div>
				<div class="heat-wrap">
					<h3>Who sings when</h3>
					<Heatmap species={heat} />
				</div>
			</div>
		</section>

		<section class="regulars page-break">
			<h2><span class="num">{numeral(4)}</span> The regulars</h2>
			<p class="dek">The {regulars.length} most-heard species, with the hours they called.</p>
			<div class="cards">
				{#each regulars as s, i (s.code)}
					<SpeciesCard {s} rank={i + 1} eager={printMode || i < 3} />
				{/each}
			</div>
		</section>

		<section class="notable page-break">
			<h2><span class="num">{numeral(5)}</span> Worth noting</h2>
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
			</div>
		</section>

		<section class="days-sec">
			<h2><span class="num">{numeral(6)}</span> Day by day</h2>
			<p class="dek">
				Detections per day and how many species made them. The busiest day was
				{day(report.totals.busiestDay, { weekday: 'long', month: 'long', day: 'numeric' })}.
			</p>
			<DailyBars days={report.daily} />
		</section>

		<section class="list">
			<h2><span class="num">{numeral(7)}</span> Every species</h2>
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
					</tr>
				</thead>
				<tbody>
					{#each report.species as s (s.code)}
						<tr>
							<td class="thumb">
								{#if s.photo}<img src={s.photo.small} alt="" loading={printMode ? 'eager' : 'lazy'} />{/if}
							</td>
							<td>
								<strong>{s.common}</strong>{#if s.isNew}<span class="dot" title="new"></span>{/if}
								<em>{s.scientific}</em>
							</td>
							<td class="r">{num(s.count)}</td>
							<td class="r">{s.daysHeard}</td>
							<td>{hour(s.peakHour)}</td>
							<td>{stamp(s.firstHeard)}</td>
							<td class="cap">{s.rarity ?? ''}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</section>

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
		td:nth-child(7) {
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
</style>
