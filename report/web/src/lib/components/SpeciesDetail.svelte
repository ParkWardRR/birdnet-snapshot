<script lang="ts">
	// One species' own view: its numbers, its hours and days, how it compares with
	// the period before, and its best recordings. A native <dialog>, so focus is
	// trapped, Escape closes it, and it opens from a link (?species=code).
	import type { Report, Species } from '../types';
	import { day, hour, num, pct, shortHour, stamp } from '../format';
	import { isNight } from '../format';
	import { player } from '../player.svelte';
	import Bars from './Bars.svelte';
	import ChangeChip from './ChangeChip.svelte';
	import RecordingCard from './RecordingCard.svelte';

	let { s, report, onclose }: { s: Species | null; report: Report; onclose: () => void } = $props();
	let dlg = $state<HTMLDialogElement>();

	$effect(() => {
		if (!dlg) return;
		if (s && !dlg.open) dlg.showModal();
		else if (!s && dlg.open) dlg.close();
	});
	// Every way of closing (button, backdrop, Escape) resets the state itself; the state
	// change then closes the dialog, so nothing depends on the native close event.
	function dismiss() {
		if (!s) return;
		if (s.clips?.some((c) => c.audio === player.src)) player.stop();
		onclose();
	}
	const hasCompare = $derived(!!report.compare);
	const dates = $derived(report.daily.map((d) => d.date));
	const hourLabels = Array.from({ length: 24 }, (_, h) => (h % 6 === 0 ? shortHour(h) : ''));
</script>

<dialog bind:this={dlg} autofocus onclose={dismiss} oncancel={(e) => { e.preventDefault(); dismiss(); }} onclick={(e) => e.target === dlg && dismiss()} aria-label={s?.common}>
	{#if s}
		<article>
			<button class="close" onclick={dismiss} aria-label="Close">×</button>
			<header>
				{#if s.photo}
					<figure>
						<img src={s.photo.large} alt={s.common} />
						{#if s.photo.author}<figcaption>Photo: {s.photo.author}{#if s.photo.license}, {s.photo.license}{/if}</figcaption>{/if}
					</figure>
				{/if}
				<div class="id">
					<h2>{s.common}</h2>
					<p class="sci">{s.scientific}</p>
					<p class="badges">
						{#if s.isNew}<span class="badge new">New to the station</span>{/if}
						{#if s.rarity}<span class="badge">{s.rarity}</span>{/if}
						{#if s.family}<span class="badge plain">{s.family}</span>{/if}
					</p>
					<dl>
						<div><dt>Detections</dt><dd>{num(s.count)} <small>{pct(s.share)} of all</small></dd></div>
						<div><dt>Days heard</dt><dd>{s.daysHeard} of {report.totals.activeDays}</dd></div>
						<div><dt>Confidence</dt><dd>{pct(s.avgConfidence)} <small>best {pct(s.maxConfidence)}</small></dd></div>
						<div><dt>Peak hour</dt><dd>{hour(s.peakHour)}</dd></div>
						<div><dt>At night</dt><dd>{pct(s.nightShare)}</dd></div>
						<div><dt>First heard</dt><dd>{stamp(s.firstHeard)}</dd></div>
					</dl>
					{#if report.compare}
						<p class="vs">
							<ChangeChip change={s.change} prev={hasCompare ? (s.prev ?? 0) : undefined} label={report.compare.label} />
							{#if s.prev}<span>{num(s.prev)} detections {report.compare.label}</span>{:else}<span>Not heard {report.compare.label}</span>{/if}
						</p>
					{/if}
				</div>
			</header>

			<h3>Hour by hour</h3>
			<Bars
				values={s.hourly}
				labels={hourLabels}
				night={isNight}
				describe={(i, v) => `${hour(i)}: ${num(v)} detections`}
			/>
			{#if s.daily.length > 1}
				<h3>Day by day</h3>
				<Bars
					values={s.daily}
					labels={dates.map((d) => day(d, { weekday: 'short' }))}
					describe={(i, v) => `${day(dates[i], { weekday: 'long', month: 'short', day: 'numeric' })}: ${num(v)} detections`}
				/>
			{/if}
			{#if s.clips?.length}
				<h3>Recordings</h3>
				<div class="recs">
					{#each s.clips as r (r.audio)}
						<RecordingCard {r} photo={s.photo} />
					{/each}
				</div>
			{/if}
		</article>
	{/if}
</dialog>

<style>
	dialog {
		border: 0;
		padding: 0;
		border-radius: 22px;
		background: var(--card);
		color: var(--ink);
		width: min(46rem, calc(100vw - 1.5rem));
		max-height: calc(100vh - 2rem);
		box-shadow: 0 30px 80px -20px rgba(20, 26, 22, 0.55);
	}
	dialog:focus {
		outline: none;
	}
	dialog::backdrop {
		background: rgba(20, 26, 22, 0.5);
		backdrop-filter: blur(3px);
	}
	article {
		padding: 1.5rem 1.6rem 1.8rem;
		position: relative;
	}
	.close {
		position: absolute;
		right: 0.9rem;
		top: 0.7rem;
		z-index: 2;
		width: 2.2rem;
		height: 2.2rem;
		border-radius: 50%;
		border: 0;
		background: rgba(251, 248, 241, 0.92);
		color: var(--ink);
		font: 400 1.5rem/1 var(--sans);
		cursor: pointer;
		box-shadow: 0 2px 10px rgba(31, 42, 36, 0.25);
	}
	header {
		display: grid;
		grid-template-columns: minmax(0, 17rem) 1fr;
		gap: 1.4rem;
		margin-bottom: 1.2rem;
	}
	figure {
		margin: 0;
	}
	figure img {
		width: 100%;
		aspect-ratio: 4 / 3;
		object-fit: cover;
		border-radius: 16px;
		display: block;
	}
	figcaption {
		font: 500 0.66rem var(--sans);
		color: var(--muted);
		margin-top: 0.3rem;
	}
	h2 {
		font: 600 1.9rem/1.1 var(--serif);
		margin: 0;
	}
	.sci {
		margin: 0.2rem 0 0.6rem;
		font: italic 400 1rem var(--serif);
		color: var(--muted);
	}
	.badges {
		display: flex;
		flex-wrap: wrap;
		gap: 0.35rem;
		margin: 0 0 0.9rem;
	}
	.badge {
		font: 600 0.62rem var(--sans);
		letter-spacing: 0.08em;
		text-transform: uppercase;
		padding: 0.28rem 0.55rem;
		border-radius: 999px;
		background: color-mix(in srgb, var(--forest) 12%, transparent);
		color: var(--forest);
	}
	.badge.new {
		background: var(--rust);
		color: #fff8ee;
	}
	.badge.plain {
		background: color-mix(in srgb, var(--ink) 6%, transparent);
		color: var(--muted);
	}
	dl {
		display: grid;
		grid-template-columns: repeat(2, 1fr);
		gap: 0.6rem 1rem;
		margin: 0;
	}
	dt {
		font: 600 0.6rem var(--sans);
		letter-spacing: 0.1em;
		text-transform: uppercase;
		color: var(--muted);
	}
	dd {
		margin: 0.1rem 0 0;
		font: 500 0.95rem var(--serif);
	}
	dd small {
		font: 400 0.72rem var(--sans);
		color: var(--muted);
	}
	.vs {
		margin: 0.9rem 0 0;
		font-size: 0.82rem;
		color: var(--muted);
		display: flex;
		gap: 0.5rem;
		align-items: center;
	}
	h3 {
		font: 600 1.05rem var(--serif);
		margin: 1.2rem 0 0.4rem;
	}
	.recs {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(15rem, 1fr));
		gap: 1rem;
	}
	@media (max-width: 640px) {
		header {
			grid-template-columns: 1fr;
		}
		article {
			padding: 1.2rem 1.1rem 1.4rem;
		}
	}
</style>
