<script lang="ts">
	// Species × hour: one dot per hour, sized by detections.
	import type { Species } from '../types';
	import { isNight, shortHour } from '../format';
	let { species }: { species: Species[] } = $props();
	const max = $derived(Math.max(1, ...species.flatMap((s) => s.hourly)));
</script>

<div class="heat" role="img" aria-label="When each species was heard">
	<div class="row head">
		<span class="name"></span>
		{#each Array(24) as _, h (h)}
			<span class="hcell">{h % 3 === 0 ? shortHour(h) : ''}</span>
		{/each}
	</div>
	{#each species as s (s.code)}
		<div class="row">
			<span class="name">{s.common}</span>
			{#each s.hourly as v, h (h)}
				<span class="hcell" class:night={isNight(h)}>
					{#if v > 0}
						<i style="--s:{0.25 + 0.75 * Math.sqrt(v / max)}" title="{s.common}, {shortHour(h)}: {v}"></i>
					{/if}
				</span>
			{/each}
		</div>
	{/each}
</div>

<style>
	.heat {
		font: 500 0.78rem/1 var(--sans);
		display: grid;
		gap: 2px;
	}
	.row {
		display: grid;
		grid-template-columns: minmax(7.5rem, 11rem) repeat(24, 1fr);
		align-items: center;
		gap: 2px;
	}
	.head .hcell {
		font-size: 0.62rem;
		color: var(--muted);
		text-align: center;
		height: 1rem;
	}
	.name {
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		padding-right: 0.5rem;
		color: var(--ink);
	}
	.hcell {
		aspect-ratio: 1;
		display: grid;
		place-items: center;
		border-radius: 3px;
		background: color-mix(in srgb, var(--gold) 9%, transparent);
	}
	.hcell.night {
		background: color-mix(in srgb, var(--night) 9%, transparent);
	}
	.head .hcell {
		background: none;
		aspect-ratio: auto;
	}
	i {
		width: calc(var(--s) * 100%);
		height: calc(var(--s) * 100%);
		border-radius: 50%;
		background: var(--forest);
	}
	.night i {
		background: var(--night);
	}
</style>
