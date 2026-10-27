<script lang="ts">
	// Weather and song: detections an hour above the temperature, over the whole
	// period, with the hours it rained shaded across both. Two panels on one time
	// axis rather than one chart with two scales.
	import type { Weather } from '../types';
	import { addDays, day, hour, num } from '../format';
	import { clamp, monotonePath, niceStep, ticks, type Pt } from '../charts';
	import { colorAt } from '../palette';
	import ChartTip, { type TipRow } from './ChartTip.svelte';

	let { w }: { w: Weather } = $props();
	const song = colorAt(0);
	const warm = colorAt(4);
	const rain = colorAt(3);

	let cw = $state(720);
	const W = $derived(clamp(cw, 340, 760));
	const L = 38;
	const R = 10;
	const T = 8;
	const hA = 96;
	const gap = 24;
	const hB = 78;
	const H = T + hA + gap + hB + 30;
	const N = $derived(w.hours.length);
	const days = $derived(N / 24);
	const plotW = $derived(W - L - R);
	const hw = $derived(plotW / N);
	const xh = (i: number) => L + i * hw;

	const maxN = $derived(Math.max(1, ...w.hours.map((h) => h.n)));
	const stepN = $derived(niceStep(maxN, 2));
	const topN = $derived(Math.max(stepN, Math.ceil(maxN / stepN) * stepN));
	const YA = (v: number) => T + hA - (v / topN) * hA;

	const temps = $derived(w.hours.flatMap((h) => (h.t === undefined ? [] : [h.t])));
	const stepT = $derived(niceStep(Math.max(...temps) - Math.min(...temps), 3));
	const loT = $derived(Math.floor(Math.min(...temps) / stepT) * stepT);
	const hiT = $derived(Math.max(loT + stepT, Math.ceil(Math.max(...temps) / stepT) * stepT));
	const topB = T + hA + gap;
	const YB = (v: number) => topB + hB - ((v - loT) / (hiT - loT)) * hB;

	// Runs of hours with a temperature, and of hours with rain.
	const tempRuns = $derived.by(() => {
		const runs: Pt[][] = [];
		let cur: Pt[] = [];
		w.hours.forEach((h, i) => {
			if (h.t === undefined) {
				if (cur.length) runs.push(cur);
				cur = [];
			} else cur.push([xh(i + 0.5), YB(h.t)]);
		});
		if (cur.length) runs.push(cur);
		return runs.filter((r) => r.length > 1);
	});
	const rainRuns = $derived.by(() => {
		const runs: [number, number][] = [];
		let start = -1;
		w.hours.forEach((h, i) => {
			if (h.r && start < 0) start = i;
			if (!h.r && start >= 0) {
				runs.push([start, i]);
				start = -1;
			}
		});
		if (start >= 0) runs.push([start, N]);
		return runs;
	});

	let hover = $state<number | null>(null);
	let tip = $state({ x: 0, y: 30, w: 720 });
	function place(i: number | null, rect: DOMRect, clientY?: number) {
		hover = i;
		if (i === null) return;
		tip = { x: (xh(i + 0.5) / W) * rect.width, y: clientY === undefined ? 40 : clientY - rect.top, w: rect.width };
	}
	function onmove(e: PointerEvent) {
		const rect = (e.currentTarget as SVGElement).getBoundingClientRect();
		const i = Math.floor((((e.clientX - rect.left) / rect.width) * W - L) / hw);
		place(i >= 0 && i < N ? i : null, rect, e.clientY);
	}
	function onkey(e: KeyboardEvent) {
		const step = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0;
		if (e.key === 'Escape') hover = null;
		if (!step) return;
		e.preventDefault();
		place(clamp((hover ?? (step > 0 ? -1 : N)) + step, 0, N - 1), (e.currentTarget as SVGElement).getBoundingClientRect());
	}
	const when = (i: number) => `${day(addDays(w.start, Math.floor(i / 24)), { weekday: 'short', month: 'short', day: 'numeric' })}, ${hour(i % 24)}`;
	const rows = $derived.by((): TipRow[] => {
		if (hover === null) return [];
		const h = w.hours[hover];
		const out: TipRow[] = [{ color: song, label: 'detections', value: num(h.n) }];
		if (h.t !== undefined) out.push({ color: warm, label: 'temperature', value: `${h.t.toFixed(1)} °C` });
		if (h.w !== undefined) out.push({ color: '#6a7266', label: 'wind', value: `${h.w.toFixed(1)} m/s` });
		return out;
	});
	const foot = $derived(hover === null ? '' : [w.hours[hover].r ? 'Rain' : w.hours[hover].o, w.hours[hover].c !== undefined ? `${w.hours[hover].c}% cloud` : ''].filter(Boolean).join(' · '));
</script>

<div class="wrap" bind:clientWidth={cw}>
	<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
	<svg
		viewBox="0 0 {W} {H}"
		role="img"
		tabindex="0"
		aria-label="Detections an hour above the temperature, over {days} days; rainy hours are shaded. Arrow keys move between hours."
		onpointermove={onmove}
		onpointerleave={() => (hover = null)}
		onblur={() => (hover = null)}
		onkeydown={onkey}
	>
		{#each rainRuns as [a, b] (a)}
			<rect class="rain" x={xh(a)} y={T} width={(b - a) * hw} height={hA + gap + hB} style="fill:{rain}" />
		{/each}
		{#each Array(Math.max(days - 1, 0)) as _, k (k)}
			<line class="sep" x1={xh((k + 1) * 24)} x2={xh((k + 1) * 24)} y1={T} y2={topB + hB} />
		{/each}

		<line class="grid" x1={L} x2={W - R} y1={YA(0)} y2={YA(0)} />
		<line class="grid" x1={L} x2={W - R} y1={YA(topN)} y2={YA(topN)} />
		<text class="ytick" x={L - 6} y={YA(0) + 3.5}>0</text>
		<text class="ytick" x={L - 6} y={YA(topN) + 3.5}>{num(topN)}</text>
		{#each w.hours as h, i (i)}
			{#if h.n > 0}
				<rect class="bar" x={xh(i) + hw * 0.14} y={YA(h.n)} width={Math.max(hw * 0.72, 1)} height={YA(0) - YA(h.n)} rx={Math.min(2, hw / 3)} style="fill:{song}" />
			{/if}
		{/each}

		{#each ticks(loT, hiT, stepT) as t (t)}
			<line class="grid" x1={L} x2={W - R} y1={YB(t)} y2={YB(t)} />
			<text class="ytick" x={L - 6} y={YB(t) + 3.5}>{t}°</text>
		{/each}
		{#each tempRuns as run, k (k)}
			<path class="line" d={monotonePath(run)} style="stroke:{warm}" />
		{/each}

		{#each Array(days) as _, k (k)}
			<text class="day" x={xh(k * 24 + 12)} y={H - 8}>{day(addDays(w.start, k), { weekday: 'short', day: 'numeric' })}</text>
		{/each}
		{#if hover !== null}
			<line class="cross" x1={xh(hover + 0.5)} x2={xh(hover + 0.5)} y1={T} y2={topB + hB} />
		{/if}
	</svg>
	{#if hover !== null}
		<ChartTip {...tip} title={when(hover)} {rows} {foot} />
	{/if}
	<ul class="legend">
		<li><i class="sw" style="background:{song}"></i>Detections an hour</li>
		<li><i class="ln" style="background:{warm}"></i>Temperature</li>
		<li><i class="sw rainy" style="background:{rain}"></i>Rain</li>
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
	.rain {
		opacity: 0.16;
	}
	.sep {
		stroke: var(--rule);
	}
	.grid {
		stroke: var(--rule);
	}
	.ytick {
		font: 500 10px var(--sans);
		fill: var(--muted);
		text-anchor: end;
	}
	.day {
		font: 500 10px var(--sans);
		fill: var(--muted);
		text-anchor: middle;
	}
	.line {
		fill: none;
		stroke-width: 2;
		stroke-linejoin: round;
		stroke-linecap: round;
	}
	.cross {
		stroke: var(--ink);
		stroke-opacity: 0.35;
	}
	.legend {
		list-style: none;
		margin: 0.7rem 0 0;
		padding: 0;
		display: flex;
		flex-wrap: wrap;
		gap: 0.3rem 1.2rem;
		font: 500 0.8rem var(--sans);
	}
	.legend li {
		display: flex;
		align-items: center;
		gap: 0.4rem;
	}
	.sw {
		width: 0.8rem;
		height: 0.55rem;
		border-radius: 3px;
	}
	.sw.rainy {
		opacity: 0.35;
	}
	.ln {
		width: 1rem;
		height: 3px;
		border-radius: 2px;
	}
</style>
