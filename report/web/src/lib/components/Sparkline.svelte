<script lang="ts">
	// A 24-hour activity strip: one bar per hour, night hours in indigo.
	import { isNight } from '../format';
	let { values, height = 28 }: { values: number[]; height?: number } = $props();
	const max = $derived(Math.max(1, ...values));
</script>

<svg viewBox="0 0 96 {height}" preserveAspectRatio="none" class="spark" aria-hidden="true">
	{#each values as v, i (i)}
		{@const h = v === 0 ? 0 : Math.max(1.5, (v / max) * (height - 2))}
		<rect x={i * 4 + 0.5} y={height - h} width="3" height={h} rx="0.8" class:night={isNight(i)} />
	{/each}
</svg>

<style>
	.spark {
		width: 100%;
		height: var(--spark-h, 28px);
		display: block;
	}
	rect {
		fill: var(--gold);
	}
	rect.night {
		fill: var(--night);
	}
</style>
