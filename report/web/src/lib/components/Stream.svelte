<script lang="ts">
	// Acoustic succession: the most-heard species hand the day to each other.
	// Each band is one species; its thickness is detections per hour, stacked
	// around a baseline chosen to keep the silhouette calm.
	import type { Species } from '../types';
	import { basisPath, clamp, niceStep, wiggle, type Pt } from '../charts';
	import { inkOn } from '../palette';
	import { hourSpan, num, shortHour } from '../format';
	import ChartTip, { type TipRow } from './ChartTip.svelte';

	let { items }: { items: { s: Species; color: string }[] } = $props();

	let cw = $state(720);
	const W = $derived(clamp(cw, 340, 760));
	const PADX = 12;
	const TOP = 8;
	const plotH = $derived(clamp(W * 0.3, 150, 220));
	const plotW = $derived(W - PADX * 2);
	const H = $derived(TOP + plotH + 30);
	const x = (h: number) => PADX + (h / 24) * plotW;

	// Layers keep the rank order, so neighbouring bands are neighbouring slots
	// of the palette (the order the colours were checked in).
	const v = $derived(items.map((it) => it.s.hourly));
	const base = $derived(wiggle(v));
	const lower = $derived(
		v.map((_, i) => base.map((b, j) => b + v.slice(0, i).reduce((a, layer) => a + layer[j], 0)))
	);
	const upper = $derived(lower.map((lo, i) => lo.map((b, j) => b + v[i][j])));
	const lo = $derived(Math.min(...lower[0]));
	const hi = $derived(Math.max(...upper[Math.max(upper.length - 1, 0)]));
	const sy = $derived((plotH - 8) / Math.max(hi - lo, 1e-9));
	const Y = (val: number) => TOP + 4 + (hi - val) * sy;

	function edge(col: number[]): Pt[] {
		return [[x(0), Y(col[0])], ...col.map((c, h): Pt => [x(h + 0.5), Y(c)]), [x(24), Y(col[23])]];
	}
	const bands = $derived(
		items.map((_, i) => {
			const top = basisPath(edge(upper[i]));
			const bottom = basisPath(edge(lower[i]).reverse()).replace(/^M/, 'L');
			return `${top}${bottom}Z`;
		})
	);

	// A name goes on its band only where the band is thick enough all the way
	// across the text; the legend carries every name regardless.
	const labelled = $derived.by(() => {
		const out: { i: number; x: number; y: number; text: string }[] = [];
		items.forEach((it, i) => {
			const text = it.s.common;
			const half = (text.length * 5.9 + 14) / 2;
			let best = -1;
			v[i].forEach((n, h) => {
				if (best < 0 || n > v[i][best]) best = h;
			});
			const cx = clamp(x(best + 0.5), PADX + half, W - PADX - half);
			const thick = (px: number) => v[i][clamp(Math.floor(((px - PADX) / plotW) * 24), 0, 23)] * sy;
			if (thick(cx - half) < 15 || thick(cx) < 15 || thick(cx + half) < 15) return;
			const cy = Y((lower[i][best] + upper[i][best]) / 2);
			if (out.some((o) => Math.abs(o.x - cx) < half + 40 && Math.abs(o.y - cy) < 13)) return;
			out.push({ i, x: cx, y: cy, text });
		});
		return out;
	});

	const peak = $derived(Math.max(1, ...Array.from({ length: 24 }, (_, h) => v.reduce((a, layer) => a + layer[h], 0))));
	const scale = $derived(niceStep(peak / 2.5, 1));

	let hour = $state<number | null>(null);
	let tip = $state({ x: 0, y: 28, w: 720 });
	function place(h: number | null, rect: DOMRect, clientY?: number) {
		hour = h;
		if (h === null) return;
		tip = { x: (x(h + 0.5) / W) * rect.width, y: clientY === undefined ? 40 : clientY - rect.top, w: rect.width };
	}
	function onmove(e: PointerEvent) {
		const rect = (e.currentTarget as SVGElement).getBoundingClientRect();
		const h = Math.floor((((e.clientX - rect.left) / rect.width) * W - PADX) / plotW * 24);
		place(h >= 0 && h < 24 ? h : null, rect, e.clientY);
	}
	function onkey(e: KeyboardEvent) {
		const step = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0;
		if (e.key === 'Escape') hour = null;
		if (!step) return;
		e.preventDefault();
		place(clamp((hour ?? (step > 0 ? -1 : 24)) + step, 0, 23), (e.currentTarget as SVGElement).getBoundingClientRect());
	}
	const tipRows = $derived.by((): TipRow[] => {
		if (hour === null) return [];
		const h = hour;
		return items
			.map((it, i) => ({ it, n: v[i][h] }))
			.filter((o) => o.n > 0)
			.sort((a, b) => b.n - a.n)
			.map((o) => ({ color: o.it.color, label: o.it.s.common, value: num(o.n) }));
	});
	const tipTotal = $derived(hour === null ? 0 : v.reduce((a, layer) => a + layer[hour!], 0));
	const labels = [0, 3, 6, 9, 12, 15, 18, 21, 24];
</script>

<div class="wrap" bind:clientWidth={cw}>
	<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
	<svg
		viewBox="0 0 {W} {H}"
		role="img"
		tabindex="0"
		aria-label="Detections per hour of day for the most-heard species, stacked as a streamgraph. Arrow keys move between hours."
		onpointermove={onmove}
		onpointerleave={() => (hour = null)}
		onblur={() => (hour = null)}
		onkeydown={onkey}
	>
		<rect class="night" x={x(0)} y={TOP} width={x(6) - x(0)} height={plotH} />
		<rect class="night" x={x(19)} y={TOP} width={x(24) - x(19)} height={plotH} />
		{#each items as it, i (it.s.code)}
			<path class="band" d={bands[i]} style="fill:{it.color}" />
		{/each}
		{#each labelled as l (l.i)}
			<text class="band-name" x={l.x} y={l.y} style="fill:{inkOn(items[l.i].color)}">{l.text}</text>
		{/each}
		<line class="axis" x1={x(0)} x2={x(24)} y1={TOP + plotH + 6} y2={TOP + plotH + 6} />
		{#each labels as h (h)}
			<text class="tick" x={x(h)} y={TOP + plotH + 22}>{shortHour(h % 24)}</text>
		{/each}
		<g class="scale">
			<line x1={PADX + 1} x2={PADX + 1} y1={TOP + 4} y2={TOP + 4 + scale * sy} />
			<line x1={PADX - 2} x2={PADX + 4} y1={TOP + 4} y2={TOP + 4} />
			<line x1={PADX - 2} x2={PADX + 4} y1={TOP + 4 + scale * sy} y2={TOP + 4 + scale * sy} />
			<text x={PADX + 9} y={TOP + 4 + (scale * sy) / 2 + 3.5}>{num(scale)} an hour</text>
		</g>
		{#if hour !== null}
			<line class="cross" x1={x(hour + 0.5)} x2={x(hour + 0.5)} y1={TOP} y2={TOP + plotH} />
		{/if}
	</svg>
	{#if hour !== null && tipRows.length}
		<ChartTip {...tip} title={hourSpan(hour)} rows={tipRows} foot="{num(tipTotal)} detections in all" />
	{/if}
	<ul class="legend">
		{#each items as it (it.s.code)}
			<li><i style="background:{it.color}"></i>{it.s.common}<small>{num(it.s.count)}</small></li>
		{/each}
	</ul>
</div>

<style>
	.wrap {
		position: relative;
	}
	svg {
		width: 100%;
		height: auto;
		display: block;
		touch-action: pan-y;
		outline-offset: 4px;
	}
	.night {
		fill: var(--night);
		opacity: 0.07;
	}
	.band {
		/* the 2px gap between touching bands is the card colour */
		stroke: var(--card);
		stroke-width: 2;
		stroke-linejoin: round;
	}
	.band-name {
		font: 600 11px var(--sans);
		text-anchor: middle;
		dominant-baseline: central;
		pointer-events: none;
	}
	.axis {
		stroke: var(--rule);
		stroke-width: 1;
	}
	.tick {
		font: 500 10px var(--sans);
		fill: var(--muted);
		text-anchor: middle;
	}
	.scale line {
		stroke: var(--muted);
		stroke-width: 1.5;
		stroke-linecap: round;
	}
	.scale text {
		font: 500 10px var(--sans);
		fill: var(--muted);
	}
	.cross {
		stroke: var(--ink);
		stroke-opacity: 0.4;
		stroke-width: 1;
	}
	.legend {
		list-style: none;
		margin: 0.7rem 0 0;
		padding: 0;
		display: flex;
		flex-wrap: wrap;
		gap: 0.35rem 1.1rem;
		font: 500 0.8rem var(--sans);
		color: var(--ink);
	}
	.legend li {
		display: flex;
		align-items: center;
		gap: 0.4rem;
	}
	.legend i {
		width: 0.8rem;
		height: 0.55rem;
		border-radius: 3px;
	}
	.legend small {
		color: var(--muted);
		font-size: 0.72rem;
	}
</style>
