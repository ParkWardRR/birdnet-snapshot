<script lang="ts">
	// How a species compares with the period before: a small arrow and a percentage.
	// Hidden below 15% either way; "new" when it was not heard then at all.
	let { change, prev, label = 'the period before' }: { change?: number | null; prev?: number; label?: string } = $props();
	const pct = $derived(change == null ? 0 : Math.round(Math.abs(change) * 100));
</script>

{#if prev === 0}
	<span class="chip up" title="Not heard {label}">▲ new</span>
{:else if change != null && Math.abs(change) >= 0.15}
	<span class="chip" class:up={change > 0} title="{change > 0 ? 'Up' : 'Down'} {pct}% per listening day on {label}">
		{change > 0 ? '▲' : '▼'} {pct}%
	</span>
{/if}

<style>
	.chip {
		display: inline-block;
		font: 600 0.66rem var(--sans);
		letter-spacing: 0.04em;
		padding: 0.14rem 0.5rem;
		border-radius: 999px;
		background: color-mix(in srgb, var(--ink) 7%, transparent);
		color: var(--muted);
		white-space: nowrap;
		vertical-align: middle;
	}
	.chip.up {
		background: color-mix(in srgb, var(--forest) 13%, transparent);
		color: var(--forest);
	}
</style>
