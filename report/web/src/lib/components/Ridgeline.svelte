<script lang="ts">
	// Who sings when: one ridge per species, its detections as a share of its
	// own day. Tall and narrow = a bird with a time; low and wide = a bird that
	// calls around the clock. Rows run from the earliest singers to the latest.
	import type { Species } from '../types';
	import { clamp, monotonePath, type Pt } from '../charts';
	import { hourSpan, num, pct, shortHour } from '../format';
	import ChartTip, { type TipRow } from './ChartTip.svelte';

	let { items }: { items: { s: Species; color: string }[] } = $props();

	let cw = $state(720);
	const W = $derived(clamp(cw, 340, 760));
	const ROW = 36;
	const AMP = 64;
	const TOP = 6;
	const AXIS = 28;
	const RIGHT = 14;

	const rows = $derived([...items].sort((a, b) => a.s.peakHour - b.s.peakHour || b.s.count - a.s.count));
	// The label column fits the longest name, within a third of the chart.
	const longest = $derived(Math.max(...rows.map((r) => r.s.common.length)));
	const left = $derived(clamp(longest * 6.1 + 26, 92, W * 0.3));
	const plotW = $derived(W - left - RIGHT);
	const baseOf = (i: number) => TOP + 6 + AMP + i * ROW; // 6px of air above the tallest ridge
	const bottom = $derived(baseOf(Math.max(rows.length - 1, 0)));
	const H = $derived(bottom + AXIS);
	const x = (h: number) => left + (h / 24) * plotW;

	const totals = $derived(rows.map((r) => r.s.hourly.reduce((a, b) => a + b, 0)));
	const dens = $derived(rows.map((r, i) => r.s.hourly.map((v) => (totals[i] ? v / totals[i] : 0))));
	const maxD = $derived(Math.max(1e-9, ...dens.flat()));

	function ridge(i: number) {
		const base = baseOf(i);
		const y = (v: number) => base - (v / maxD) * AMP;
		const d = dens[i];
		const pts: Pt[] = [[x(0), y(d[0])], ...d.map((v, h): Pt => [x(h + 0.5), y(v)]), [x(24), y(d[23])]];
		const line = monotonePath(pts);
		return { line, area: `${line}L${x(24)},${base}L${x(0)},${base}Z` };
	}
	const ridges = $derived(rows.map((_, i) => ridge(i)));

	const maxChars = $derived(Math.floor((left - 22) / 6.1));
	const clip = (s: string) => (s.length > maxChars ? s.slice(0, Math.max(maxChars - 1, 1)) + '…' : s);

	// Hover and keyboard: a crosshair on the hour, a readout of every species.
	let hour = $state<number | null>(null);
	let tip = $state({ x: 0, y: 24, w: 720 });
	function place(h: number | null, rect: DOMRect, clientY?: number) {
		hour = h;
		if (h === null) return;
		tip = { x: (x(h + 0.5) / W) * rect.width, y: clientY === undefined ? 28 : clientY - rect.top, w: rect.width };
	}
	function onmove(e: PointerEvent) {
		const rect = (e.currentTarget as SVGElement).getBoundingClientRect();
		const h = Math.floor((((e.clientX - rect.left) / rect.width) * W - left) / plotW * 24);
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
		return rows
			.map((r, i) => ({ r, n: r.s.hourly[h], i }))
			.filter((o) => o.n > 0)
			.sort((a, b) => b.n - a.n)
			.map((o) => ({ color: o.r.color, label: o.r.s.common, value: `${num(o.n)} · ${pct(o.n / totals[o.i])}` }));
	});
	const labels = [0, 3, 6, 9, 12, 15, 18, 21, 24];
</script>

<div class="wrap" bind:clientWidth={cw}>
	<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
	<svg
		viewBox="0 0 {W} {H}"
		role="img"
		tabindex="0"
		aria-label="Share of each species' detections by hour of day, one ridge per species. Arrow keys move between hours."
		onpointermove={onmove}
		onpointerleave={() => (hour = null)}
		onblur={() => (hour = null)}
		onkeydown={onkey}
	>
		<rect class="night" x={x(0)} y={TOP} width={x(6) - x(0)} height={bottom - TOP} />
		<rect class="night" x={x(19)} y={TOP} width={x(24) - x(19)} height={bottom - TOP} />
		{#each rows as r, i (r.s.code)}
			<line class="base" x1={x(0)} x2={x(24)} y1={baseOf(i)} y2={baseOf(i)} />
			<path class="fill" d={ridges[i].area} style="fill:{r.color}" />
			<path class="line" d={ridges[i].line} style="stroke:{r.color}" />
			<rect x="2" y={baseOf(i) - 9} width="10" height="3" rx="1.5" fill={r.color} />
			<text class="name" x="18" y={baseOf(i) - 4}>{clip(r.s.common)}</text>
		{/each}
		{#each labels as h (h)}
			<text class="tick" x={x(h)} y={bottom + 18}>{shortHour(h % 24)}</text>
		{/each}
		{#if hour !== null}
			<line class="cross" x1={x(hour + 0.5)} x2={x(hour + 0.5)} y1={TOP} y2={bottom} />
		{/if}
	</svg>
	{#if hour !== null && tipRows.length}
		<ChartTip {...tip} title={hourSpan(hour)} rows={tipRows} foot="share of that species' detections" />
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
	.night {
		fill: var(--night);
		opacity: 0.07;
	}
	.base {
		stroke: var(--rule);
		stroke-width: 1;
	}
	.fill {
		opacity: 0.18;
	}
	.line {
		fill: none;
		stroke-width: 2;
		stroke-linejoin: round;
		stroke-linecap: round;
	}
	.name {
		font: 600 11px var(--sans);
		fill: var(--ink);
	}
	.tick {
		font: 500 10px var(--sans);
		fill: var(--muted);
		text-anchor: middle;
	}
	.cross {
		stroke: var(--ink);
		stroke-opacity: 0.35;
		stroke-width: 1;
	}
</style>
