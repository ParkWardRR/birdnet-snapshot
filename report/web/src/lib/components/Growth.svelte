<script lang="ts">
	// The species list over time: how many species the station has heard in total,
	// by day. A step line, because a species is either on the list or not.
	import type { Growth } from '../types';
	import { clamp, niceStep, ticks } from '../charts';
	import { day, num } from '../format';
	import { colorAt } from '../palette';
	import ChartTip, { type TipRow } from './ChartTip.svelte';

	let { g }: { g: Growth } = $props();
	const color = colorAt(0);

	let cw = $state(720);
	const W = $derived(clamp(cw, 340, 760));
	const H = 230;
	const L = 34;
	const R = 70;
	const T = 16;
	const B = 28;
	const plotW = $derived(W - L - R);
	const plotH = H - T - B;
	const n = $derived(g.days.length);
	const x = (i: number) => L + (i / Math.max(n - 1, 1)) * plotW;
	const last = $derived(g.days[n - 1]);
	const step = $derived(niceStep(last.total, 4));
	const top = $derived(Math.max(step, Math.ceil(last.total / step) * step));
	const Y = (v: number) => T + (1 - v / top) * plotH;
	const grid = $derived(ticks(0, top, step));
	const line = $derived(g.days.map((d, i) => (i === 0 ? `M${x(0)},${Y(d.total)}` : `H${x(i)}V${Y(d.total)}`)).join(''));
	const every = $derived(Math.max(1, Math.ceil(n / 6)));

	let hover = $state<number | null>(null);
	let tip = $state({ x: 0, y: 30, w: 720 });
	function place(i: number | null, rect: DOMRect, clientY?: number) {
		hover = i;
		if (i === null) return;
		tip = { x: (x(i) / W) * rect.width, y: clientY === undefined ? 40 : clientY - rect.top, w: rect.width };
	}
	function onmove(e: PointerEvent) {
		const rect = (e.currentTarget as SVGElement).getBoundingClientRect();
		const vx = ((e.clientX - rect.left) / rect.width) * W;
		place(clamp(Math.round(((vx - L) / plotW) * (n - 1)), 0, n - 1), rect, e.clientY);
	}
	function onkey(e: KeyboardEvent) {
		const step = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0;
		if (e.key === 'Escape') hover = null;
		if (!step) return;
		e.preventDefault();
		place(clamp((hover ?? (step > 0 ? -1 : n)) + step, 0, n - 1), (e.currentTarget as SVGElement).getBoundingClientRect());
	}
	const rows = $derived.by((): TipRow[] => (hover === null ? [] : [{ color, label: 'species so far', value: num(g.days[hover].total) }]));
	const foot = $derived(hover === null ? '' : g.days[hover].new ? `New: ${g.days[hover].species?.join(', ')}` : 'No new species that day');
</script>

<div class="wrap" bind:clientWidth={cw}>
	<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
	<svg
		viewBox="0 0 {W} {H}"
		role="img"
		tabindex="0"
		aria-label="Species heard in total, by day, rising to {last.total}. Arrow keys move between days."
		onpointermove={onmove}
		onpointerleave={() => (hover = null)}
		onblur={() => (hover = null)}
		onkeydown={onkey}
	>
		{#each grid as t (t)}
			<line class="grid" x1={L} x2={W - R + 8} y1={Y(t)} y2={Y(t)} />
			<text class="ytick" x={L - 6} y={Y(t) + 3.5}>{t}</text>
		{/each}
		{#each g.days as d, i (d.date)}
			{#if i % every === 0}
				<text class="xtick" x={x(i)} y={H - 8}>{day(d.date, { month: 'short', day: 'numeric' })}</text>
			{/if}
		{/each}
		<path class="area" d="{line}V{Y(0)}H{x(0)}Z" style="fill:{color}" />
		<path class="line" d={line} style="stroke:{color}" />
		{#each g.days as d, i (d.date)}
			{#if d.new > 0 && n <= 120}<circle class="dot" cx={x(i)} cy={Y(d.total)} r="4.5" style="fill:{color}" />{/if}
		{/each}
		<text class="end" x={x(n - 1) + 9} y={Y(last.total) + 4}>{last.total} species</text>
		{#if hover !== null}
			<line class="cross" x1={x(hover)} x2={x(hover)} y1={T} y2={T + plotH} />
		{/if}
	</svg>
	{#if hover !== null}
		<ChartTip {...tip} title={day(g.days[hover].date, { weekday: 'short', month: 'short', day: 'numeric' })} {rows} {foot} />
	{/if}
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
	.grid {
		stroke: var(--rule);
	}
	.ytick {
		font: 500 10px var(--sans);
		fill: var(--muted);
		text-anchor: end;
	}
	.xtick {
		font: 500 10px var(--sans);
		fill: var(--muted);
		text-anchor: middle;
	}
	.area {
		opacity: 0.1;
	}
	.line {
		fill: none;
		stroke-width: 2;
		stroke-linejoin: round;
	}
	.dot {
		stroke: var(--card);
		stroke-width: 2;
	}
	.end {
		font: 600 11px var(--sans);
		fill: var(--ink);
	}
	.cross {
		stroke: var(--ink);
		stroke-opacity: 0.35;
	}
</style>
