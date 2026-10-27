<script lang="ts">
	// A small bar chart for the species view: one series, one baseline, a few
	// labels. Night hours are indigo when `night` is given.
	let {
		values,
		labels = [],
		night,
		describe,
		height = 104
	}: {
		values: number[];
		labels?: string[];
		night?: (i: number) => boolean;
		describe: (i: number, v: number) => string;
		height?: number;
	} = $props();
	const W = 600;
	const base = $derived(height - 20);
	const max = $derived(Math.max(1, ...values));
	const slot = $derived(W / Math.max(values.length, 1));
	const bw = $derived(Math.min(slot * 0.72, 30));
</script>

<svg viewBox="0 0 {W} {height}" role="img" aria-label={values.map((v, i) => describe(i, v)).join('; ')}>
	<line class="base" x1="0" x2={W} y1={base} y2={base} />
	{#each values as v, i (i)}
		{@const h = v ? Math.max(2, (v / max) * (base - 8)) : 0}
		<rect class:night={night?.(i)} x={i * slot + (slot - bw) / 2} y={base - h} width={bw} height={h} rx={Math.min(3, bw / 2)}>
			<title>{describe(i, v)}</title>
		</rect>
		{#if labels[i]}<text class="tick" x={i * slot + slot / 2} y={height - 3}>{labels[i]}</text>{/if}
	{/each}
</svg>

<style>
	svg {
		width: 100%;
		height: auto;
		display: block;
	}
	.base {
		stroke: var(--rule);
	}
	rect {
		fill: var(--forest);
	}
	rect.night {
		fill: var(--night);
	}
	.tick {
		font: 500 12px var(--sans);
		fill: var(--muted);
		text-anchor: middle;
	}
</style>
