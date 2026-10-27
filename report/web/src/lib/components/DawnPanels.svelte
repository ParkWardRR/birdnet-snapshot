<script lang="ts">
	// Dawn chorus onset: for each morning singer, how many minutes after civil
	// dawn it started, day by day. One small panel per species on a shared
	// scale, so a species starting earlier reads as lower in every panel.
	import type { OnsetSeries } from '../types';
	import { niceStep, ticks, type Pt } from '../charts';
	import { day, num, offset } from '../format';
	import ChartTip from './ChartTip.svelte';

	let { series, colorOf }: { series: OnsetSeries[]; colorOf: (code: string) => string } = $props();

	const PW = 300;
	const PH = 176;
	const L = 38;
	const R = 12;
	const T = 12;
	const B = 28;
	const plotW = PW - L - R;
	const plotH = PH - T - B;

	const values = $derived(series.flatMap((s) => s.days.map((d) => d.minutes).filter((m): m is number => m !== null)));
	const step = $derived(niceStep(Math.max(...values, 0) - Math.min(...values, 0), 3));
	const yMin = $derived(Math.min(0, ...values) - step * 0.35);
	const yMax = $derived(Math.max(0, ...values) + step * 0.35);
	const Y = (m: number) => T + ((yMax - m) / (yMax - yMin)) * plotH;
	const grid = $derived(ticks(yMin, yMax, step));
	const sign = (t: number) => (t === 0 ? 'dawn' : `${t > 0 ? '+' : '−'}${Math.abs(t)}`);

	const dates = $derived(series[0]?.days.map((d) => d.date) ?? []);
	const X = (i: number) => L + ((i + 0.5) / Math.max(dates.length, 1)) * plotW;
	const every = $derived(dates.length > 7 ? 2 : 1);

	// Runs of consecutive days with an onset: the line breaks over a gap.
	function runs(s: OnsetSeries): Pt[][] {
		const out: Pt[][] = [];
		let cur: Pt[] = [];
		s.days.forEach((d, i) => {
			if (d.minutes === null) {
				if (cur.length) out.push(cur);
				cur = [];
			} else {
				cur.push([X(i), Y(d.minutes)]);
			}
		});
		if (cur.length) out.push(cur);
		return out;
	}

	let tip = $state<{ si: number; di: number; x: number; y: number; w: number } | null>(null);
	function show(e: Event, si: number, di: number, cx: number, cy: number) {
		const svg = (e.currentTarget as SVGElement).ownerSVGElement!;
		const rect = svg.getBoundingClientRect();
		tip = { si, di, x: (cx / PW) * rect.width, y: (cy / PH) * rect.height, w: rect.width };
	}
</script>

<div class="panels">
	{#each series as s, si (s.code)}
		{@const color = colorOf(s.code)}
		<figure>
			<figcaption><i style="background:{color}"></i>{s.common}</figcaption>
			<div class="wrap">
				<svg viewBox="0 0 {PW} {PH}" role="img" aria-label="{s.common}: minutes after civil dawn it started singing, by day">
					{#each grid as t (t)}
						<line class={t === 0 ? 'zero' : 'grid'} x1={L} x2={PW - R} y1={Y(t)} y2={Y(t)} />
						<text class="ytick" x={L - 6} y={Y(t) + 3.5}>{sign(t)}</text>
					{/each}
					{#each dates as dt, i (dt)}
						{#if i % every === 0}
							<text class="xtick" x={X(i)} y={PH - 10}>{day(dt, { weekday: 'short' })}</text>
						{/if}
					{/each}
					{#each runs(s) as run, k (k)}
						{#if run.length > 1}
							<path class="line" d={run.map(([x, y], j) => `${j ? 'L' : 'M'}${x},${y}`).join('')} style="stroke:{color}" />
						{/if}
					{/each}
					{#each s.days as d, di (d.date)}
						{#if d.minutes !== null}
							<circle class="dot" cx={X(di)} cy={Y(d.minutes)} r="4.5" style="fill:{color}" />
							<!-- a hit area much larger than the dot -->
							<circle
								class="hit"
								cx={X(di)}
								cy={Y(d.minutes)}
								r="16"
								role="presentation"
								onpointerenter={(e) => show(e, si, di, X(di), Y(d.minutes!))}
								onpointerdown={(e) => show(e, si, di, X(di), Y(d.minutes!))}
								onpointerleave={() => (tip = null)}
							/>
						{/if}
					{/each}
				</svg>
				{#if tip && tip.si === si}
					{@const d = s.days[tip.di]}
					<ChartTip
						x={tip.x}
						y={tip.y}
						w={tip.w}
						title={day(d.date, { weekday: 'long', month: 'short', day: 'numeric' })}
						rows={[{ color, label: s.common, value: offset(d.minutes!) }]}
						foot="{num(d.count)} detections that day"
					/>
				{/if}
			</div>
		</figure>
	{/each}
</div>

<style>
	.panels {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(12.5rem, 1fr));
		gap: 1.1rem 1.4rem;
	}
	figure {
		margin: 0;
		min-width: 0;
	}
	figcaption {
		display: flex;
		align-items: center;
		gap: 0.45rem;
		font: 600 0.92rem var(--serif);
		color: var(--ink);
		margin-bottom: 0.2rem;
	}
	figcaption i {
		width: 0.8rem;
		height: 3px;
		border-radius: 2px;
	}
	.wrap {
		position: relative;
		max-width: 26rem;
	}
	svg {
		width: 100%;
		height: auto;
		display: block;
		overflow: visible;
	}
	.grid {
		stroke: var(--rule);
		stroke-width: 1;
	}
	.zero {
		stroke: var(--muted);
		stroke-opacity: 0.6;
		stroke-width: 1;
	}
	.ytick {
		font: 500 9.5px var(--sans);
		fill: var(--muted);
		text-anchor: end;
	}
	.xtick {
		font: 500 9.5px var(--sans);
		fill: var(--muted);
		text-anchor: middle;
	}
	.line {
		fill: none;
		stroke-width: 2;
		stroke-linejoin: round;
		stroke-linecap: round;
	}
	.dot {
		stroke: var(--card);
		stroke-width: 2;
	}
	.hit {
		fill: transparent;
		cursor: default;
	}
	@media print {
		.panels {
			grid-template-columns: repeat(2, 1fr);
		}
		.wrap {
			max-width: none;
		}
	}
</style>
