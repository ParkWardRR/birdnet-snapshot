<script lang="ts">
	// Detections per day (bars) with the number of species above each.
	import { day, num } from '../format';
	let { days }: { days: { date: string; detections: number; species: number }[] } = $props();
	const max = $derived(Math.max(1, ...days.map((d) => d.detections)));
</script>

<div class="days" style="--n:{days.length}">
	{#each days as d (d.date)}
		<div class="day">
			<span class="sp">{d.species ? `${d.species} sp.` : ''}</span>
			<div class="track">
				<div class="fill" style="height:{(d.detections / max) * 100}%"></div>
			</div>
			<span class="count">{d.detections ? num(d.detections) : '–'}</span>
			<span class="label">{day(d.date, { weekday: 'short' })}<br /><small>{day(d.date, { month: 'short', day: 'numeric' })}</small></span>
		</div>
	{/each}
</div>

<style>
	.days {
		display: grid;
		grid-template-columns: repeat(var(--n), minmax(0, 7rem));
		justify-content: start;
		gap: 0.6rem;
		align-items: end;
	}
	.day {
		display: grid;
		gap: 0.35rem;
		text-align: center;
		font: 500 0.8rem var(--sans);
	}
	.sp {
		color: var(--forest);
		font-size: 0.72rem;
		min-height: 1em;
	}
	.track {
		height: 150px;
		display: flex;
		align-items: flex-end;
		background: color-mix(in srgb, var(--ink) 4%, transparent);
		border-radius: 8px;
		overflow: hidden;
	}
	.fill {
		width: 100%;
		background: linear-gradient(180deg, var(--forest-light), var(--forest));
		border-radius: 8px 8px 0 0;
		min-height: 2px;
	}
	.count {
		font: 600 0.95rem var(--serif);
		color: var(--ink);
	}
	.label {
		color: var(--muted);
		text-transform: uppercase;
		letter-spacing: 0.08em;
		font-size: 0.68rem;
		line-height: 1.3;
	}
	small {
		text-transform: none;
		letter-spacing: 0;
	}
</style>
