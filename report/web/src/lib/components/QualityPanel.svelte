<script lang="ts">
	// How much to trust the period: how many detections two models agreed on,
	// how confident they were, how many a person threw out, and the species that
	// deserve a pinch of salt.
	import type { Quality } from '../types';
	import { num, pct } from '../format';
	import ChartTable from './ChartTable.svelte';
	let { q }: { q: Quality } = $props();
	const max = $derived(Math.max(1, ...q.histogram));
	const lo = (i: number) => 20 + i * 10;
	const raw = $derived(q.kept + q.removed);
	const bucket = (i: number) => `${lo(i)}–${i === 7 ? 100 : lo(i) + 10}%`;
</script>

<div class="tiles">
	<div class="tile"><span class="v">{pct(q.twoModel)}</span><span class="l">confirmed by two or more models</span></div>
	<div class="tile"><span class="v">{pct(q.median)}</span><span class="l">median confidence</span></div>
	<div class="tile"><span class="v">{pct(q.high)}</span><span class="l">at 80% confidence or more</span></div>
	<div class="tile">
		<span class="v">{num(q.removed)}</span>
		<span class="l">reviewed and removed{#if raw}<br /><small>{pct(q.removed / raw)} of everything the models flagged</small>{/if}</span>
	</div>
</div>

<div class="cols">
	<div>
		<h3>How confident the models were</h3>
		<div class="hist" role="img" aria-label="Detections by confidence">
			{#each q.histogram as n, i (i)}
				<div class="col">
					<span class="n">{n === max ? num(n) : ''}</span>
					<div class="bar" style="height:{(n / max) * 100}%" title="{num(n)} detections at {bucket(i)}"></div>
					<span class="x">{lo(i)}</span>
				</div>
			{/each}
		</div>
		<p class="axis">Confidence, %</p>
	</div>
	<div>
		<h3>Treat with caution</h3>
		{#if q.cautions?.length}
			<ul class="cautions">
				{#each q.cautions as c (c.code)}
					<li>
						<strong>{c.common}</strong>
						<span>{num(c.count)} detections, {c.reason}. Mean confidence {pct(c.mean)}, {pct(c.twoModel)} confirmed by a second model.</span>
					</li>
				{/each}
			</ul>
		{:else}
			<p class="none">No species stand out as doubtful this period.</p>
		{/if}
	</div>
</div>

<ChartTable
	label="Show as a table"
	corner="Confidence"
	head={q.histogram.map((_, i) => bucket(i))}
	rows={[{ name: 'Detections', cells: q.histogram.map((n) => num(n)) }]}
/>

<style>
	.tiles {
		display: grid;
		grid-template-columns: repeat(4, 1fr);
		background: var(--card);
		border: 1px solid var(--rule);
		border-radius: 20px;
		margin-bottom: 1.3rem;
	}
	.tile {
		padding: 1.1rem 1.3rem;
		display: grid;
		gap: 0.2rem;
		border-left: 1px solid var(--rule);
		align-content: start;
	}
	.tile:first-child {
		border-left: 0;
	}
	.v {
		font: 600 clamp(1.5rem, 3vw, 2.1rem) / 1 var(--serif);
		color: var(--forest);
	}
	.l {
		font: 600 0.66rem var(--sans);
		letter-spacing: 0.1em;
		text-transform: uppercase;
		color: var(--muted);
	}
	.l small {
		text-transform: none;
		letter-spacing: 0;
		font-weight: 500;
		font-size: 0.72rem;
	}
	.cols {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 1.3rem;
	}
	.cols > div {
		background: var(--card);
		border: 1px solid var(--rule);
		border-radius: 20px;
		padding: 1.2rem 1.3rem;
		break-inside: avoid;
	}
	h3 {
		margin: 0 0 0.8rem;
	}
	.hist {
		display: grid;
		grid-template-columns: repeat(8, 1fr);
		gap: 6px;
		height: 9rem;
		align-items: end;
	}
	.col {
		display: grid;
		grid-template-rows: 1rem 1fr 1rem;
		height: 100%;
		text-align: center;
	}
	.bar {
		align-self: end;
		background: var(--forest);
		border-radius: 4px 4px 0 0;
		min-height: 2px;
		max-width: 1.5rem;
		justify-self: center;
		width: 100%;
	}
	.n {
		font: 600 0.7rem var(--sans);
		color: var(--ink);
	}
	.x {
		font: 500 0.68rem var(--sans);
		color: var(--muted);
		padding-top: 0.2rem;
	}
	.axis {
		margin: 0.3rem 0 0;
		text-align: center;
		font: 500 0.68rem var(--sans);
		color: var(--muted);
	}
	.cautions {
		list-style: none;
		margin: 0;
		padding: 0;
	}
	.cautions li {
		padding: 0.55rem 0;
		border-top: 1px solid var(--rule);
	}
	.cautions li:first-child {
		border-top: 0;
		padding-top: 0;
	}
	.cautions strong {
		display: block;
		font: 600 0.98rem var(--serif);
	}
	.cautions span,
	.none {
		font-size: 0.82rem;
		color: var(--muted);
	}
	@media (max-width: 760px) {
		.tiles {
			grid-template-columns: repeat(2, 1fr);
		}
		.tile:nth-child(3) {
			border-left: 0;
		}
		.tile:nth-child(n + 3) {
			border-top: 1px solid var(--rule);
		}
		.cols {
			grid-template-columns: 1fr;
		}
	}
</style>
