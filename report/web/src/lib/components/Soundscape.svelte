<script lang="ts">
	// The station's sound by hour of day and octave band: a heatmap that reads
	// like a spectrogram of the day, with the birds heard each hour above it.
	// Levels are BirdNET-Go's own scale, so only the pattern is meant.
	import type { Soundscape } from '../types';
	import { clamp } from '../charts';
	import { hour, num, shortHour } from '../format';
	import { colorAt } from '../palette';
	import ChartTip, { type TipRow } from './ChartTip.svelte';

	let { s, hourly }: { s: Soundscape; hourly: number[] } = $props();
	const birds = colorAt(2);
	const quiet = '#eef3e8';
	const loud = '#1f4a2b';

	let cw = $state(720);
	const W = $derived(clamp(cw, 340, 760));
	const L = 56;
	const R = 12;
	const strip = 40;
	const gap = 10;
	const rowH = 17;
	const axis = 24;
	const plotW = $derived(W - L - R);
	const cell = $derived(plotW / 24);
	const rows = $derived(s.bands.length);
	const top = strip + gap;
	const H = $derived(top + rows * rowH + axis);
	const maxBirds = $derived(Math.max(1, ...hourly));
	// Each pitch is shaded against its own quietest and loudest hour, so the daily
	// rhythm of every band shows (the low bands are far louder overall, which would
	// otherwise paint them all dark). Exact levels are in the readout and the table.
	const range = $derived(
		s.levels.map((row) => {
			const vs = row.filter((v): v is number => v !== null);
			return [Math.min(...vs), Math.max(...vs)] as const;
		})
	);
	const shade = (v: number, b: number) => {
		const [lo, hi] = range[b];
		const p = clamp((v - lo) / Math.max(hi - lo, 1e-9), 0, 1);
		return `color-mix(in oklab, ${loud} ${Math.round(Math.pow(p, 0.9) * 100)}%, ${quiet})`;
	};
	// The lowest band sits at the bottom, as on a spectrogram.
	const rowOf = (b: number) => rows - 1 - b;

	let hover = $state<{ b: number; h: number } | null>(null);
	let tip = $state({ x: 0, y: 30, w: 720 });
	function onmove(e: PointerEvent) {
		const rect = (e.currentTarget as SVGElement).getBoundingClientRect();
		const vx = ((e.clientX - rect.left) / rect.width) * W;
		const vy = ((e.clientY - rect.top) / rect.width) * W;
		const h = Math.floor((vx - L) / cell);
		const row = Math.floor((vy - top) / rowH);
		if (h < 0 || h > 23 || row < 0 || row >= rows) {
			hover = null;
			return;
		}
		hover = { b: rows - 1 - row, h };
		tip = { x: e.clientX - rect.left, y: e.clientY - rect.top, w: rect.width };
	}
	const tipRows = $derived.by((): TipRow[] => {
		if (!hover) return [];
		const v = s.levels[hover.b][hover.h];
		return [{ color: shade(v ?? s.levels[hover.b].find((x) => x !== null) ?? 0, hover.b), label: s.bands[hover.b], value: v === null ? 'no data' : `${v.toFixed(1)} dB` }];
	});
	const ticks = [0, 3, 6, 9, 12, 15, 18, 21];
</script>

<div class="wrap" bind:clientWidth={cw}>
	<svg
		viewBox="0 0 {W} {H}"
		role="img"
		aria-label="Sound level by hour of day and octave band. Quietest at {hour(s.quietest)}, loudest at {hour(s.loudest)}."
		onpointermove={onmove}
		onpointerleave={() => (hover = null)}
	>
		<text class="label" x={L - 8} y={strip - 6}>birds</text>
		{#each hourly as n, h (h)}
			{#if n > 0}
				<rect class="bird" x={L + h * cell + cell * 0.12} y={strip - (n / maxBirds) * (strip - 6)} width={cell * 0.76} height={(n / maxBirds) * (strip - 6)} rx="2" style="fill:{birds}">
					<title>{num(n)} detections, {hour(h)}</title>
				</rect>
			{/if}
		{/each}
		{#each s.levels as row, b (b)}
			{#each row as v, h (h)}
				<rect class="cell" x={L + h * cell} y={top + rowOf(b) * rowH} width={cell + 0.5} height={rowH + 0.5} style="fill:{v === null ? 'transparent' : shade(v, b)}" />
			{/each}
			<text class="label" x={L - 8} y={top + rowOf(b) * rowH + rowH / 2 + 3.5}>{s.bands[b]}</text>
		{/each}
		{#each ticks as h (h)}
			<text class="tick" x={L + h * cell} y={top + rows * rowH + 16}>{shortHour(h)}</text>
		{/each}
		{#if hover}
			<rect class="focus" x={L + hover.h * cell} y={top + rowOf(hover.b) * rowH} width={cell} height={rowH} />
		{/if}
	</svg>
	{#if hover}
		<ChartTip {...tip} title="{hour(hover.h)}, {s.bands[hover.b]}" rows={tipRows} foot="{num(hourly[hover.h] ?? 0)} detections that hour" />
	{/if}
	<div class="ramp">
		<span>that pitch's quietest hour</span>
		<i style="background: linear-gradient(90deg, {quiet}, {loud})"></i>
		<span>its loudest</span>
		<span class="scale">Levels are BirdNET-Go's own scale ({s.min.toFixed(0)} to {s.max.toFixed(0)} dB); hover for exact values</span>
	</div>
</div>

<style>
	.wrap {
		position: relative;
	}
	svg {
		width: 100%;
		height: auto;
		display: block;
	}
	.label {
		font: 500 10px var(--sans);
		fill: var(--muted);
		text-anchor: end;
	}
	.tick {
		font: 500 10px var(--sans);
		fill: var(--muted);
		text-anchor: middle;
	}
	.cell {
		shape-rendering: crispEdges;
	}
	.focus {
		fill: none;
		stroke: var(--ink);
		stroke-width: 1.5;
		pointer-events: none;
	}
	.ramp {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 0.5rem;
		margin-top: 0.6rem;
		font: 500 0.74rem var(--sans);
		color: var(--muted);
	}
	.ramp i {
		width: 7rem;
		height: 0.55rem;
		border-radius: 3px;
		border: 1px solid var(--rule);
	}
	.scale {
		margin-left: auto;
	}
</style>
