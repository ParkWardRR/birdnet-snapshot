<script lang="ts" module>
	export type TipRow = { color: string; label: string; value: string };
</script>

<script lang="ts">
	// The readout that follows the pointer over a chart: values lead, names
	// follow, rows keyed with a short line in the series colour.
	let {
		x,
		y,
		w,
		title,
		rows,
		foot
	}: { x: number; y: number; w: number; title: string; rows: TipRow[]; foot?: string } = $props();
	const right = $derived(x > w / 2);
</script>

<div
	class="tip"
	role="status"
	style="left:{right ? x - 14 : x + 14}px; top:{y}px; transform: translate({right ? '-100%' : '0'}, -50%)"
>
	<p class="title">{title}</p>
	{#each rows as r (r.label)}
		<p class="row">
			<i style="background:{r.color}"></i>
			<strong>{r.value}</strong>
			<span>{r.label}</span>
		</p>
	{/each}
	{#if foot}<p class="foot">{foot}</p>{/if}
</div>

<style>
	.tip {
		position: absolute;
		z-index: 5;
		pointer-events: none;
		min-width: 9.5rem;
		max-width: 15rem;
		padding: 0.55rem 0.75rem;
		border-radius: 10px;
		background: rgba(31, 42, 36, 0.95);
		color: #f4efe4;
		box-shadow: 0 10px 28px -12px rgba(31, 42, 36, 0.7);
		font: 500 0.74rem/1.35 var(--sans);
	}
	p {
		margin: 0;
	}
	.title {
		font: 600 0.7rem var(--sans);
		letter-spacing: 0.06em;
		text-transform: uppercase;
		color: #d8d0bc;
		margin-bottom: 0.3rem;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 0.45rem;
		padding: 0.08rem 0;
	}
	i {
		flex: none;
		width: 0.7rem;
		height: 3px;
		border-radius: 2px;
	}
	strong {
		font: 600 0.82rem var(--sans);
		color: #fff;
	}
	span {
		color: #d8d0bc;
	}
	.foot {
		margin-top: 0.3rem;
		padding-top: 0.3rem;
		border-top: 1px solid rgba(244, 239, 228, 0.18);
		color: #d8d0bc;
	}
</style>
