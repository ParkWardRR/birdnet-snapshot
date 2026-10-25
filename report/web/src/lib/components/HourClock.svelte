<script lang="ts">
	// Detections by hour on a 24-hour dial: midnight at the top, bars grow
	// outward, night hours in indigo.
	import { isNight, num } from '../format';
	let { values, total }: { values: number[]; total: number } = $props();
	const max = $derived(Math.max(1, ...values));
	const R0 = 62;
	const R1 = 128;
	function bar(i: number, v: number) {
		const a0 = ((i - 0.42) / 24) * Math.PI * 2 - Math.PI / 2;
		const a1 = ((i + 0.42) / 24) * Math.PI * 2 - Math.PI / 2;
		const r = R0 + (v / max) * (R1 - R0) + (v > 0 ? 3 : 0);
		const p = (rr: number, a: number) => `${150 + rr * Math.cos(a)},${150 + rr * Math.sin(a)}`;
		return `M${p(R0, a0)} L${p(r, a0)} A${r},${r} 0 0 1 ${p(r, a1)} L${p(R0, a1)} A${R0},${R0} 0 0 0 ${p(R0, a0)} Z`;
	}
	const labels = [0, 6, 12, 18];
	const labelText = ['midnight', '6 am', 'noon', '6 pm'];
</script>

<svg viewBox="0 0 300 300" class="clock" role="img" aria-label="Detections by hour of day">
	<circle cx="150" cy="150" r={R1 + 4} class="ring" />
	<circle cx="150" cy="150" r={R0 - 2} class="hub" />
	{#each values as v, i (i)}
		<path d={bar(i, v)} class:night={isNight(i)} class="bar">
			<title>{i}:00 – {num(v)} detections</title>
		</path>
	{/each}
	{#each labels as h, k (h)}
		{@const a = (h / 24) * Math.PI * 2 - Math.PI / 2}
		<text x={150 + (R1 + 16) * Math.cos(a)} y={150 + (R1 + 16) * Math.sin(a) + 3.5} class="tick">{labelText[k]}</text>
	{/each}
	<text x="150" y="146" class="total">{num(total)}</text>
	<text x="150" y="164" class="caption">detections</text>
</svg>

<style>
	.clock {
		width: 100%;
		max-width: 340px;
		display: block;
		margin: 0 auto;
		overflow: visible;
	}
	.ring {
		fill: none;
		stroke: var(--rule);
		stroke-dasharray: 2 4;
	}
	.hub {
		fill: var(--card);
		stroke: var(--rule);
	}
	.bar {
		fill: var(--gold);
	}
	.bar.night {
		fill: var(--night);
	}
	.tick {
		font: 600 9px var(--sans);
		letter-spacing: 0.08em;
		text-transform: uppercase;
		fill: var(--muted);
		text-anchor: middle;
	}
	.total {
		font: 600 26px var(--serif);
		fill: var(--ink);
		text-anchor: middle;
	}
	.caption {
		font: 500 9px var(--sans);
		letter-spacing: 0.14em;
		text-transform: uppercase;
		fill: var(--muted);
		text-anchor: middle;
	}
</style>
