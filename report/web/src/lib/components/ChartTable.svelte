<script lang="ts">
	// The table twin of a chart: every value the chart draws, readable without
	// hovering, seeing colour, or using a pointer. Collapsed, and not printed.
	let {
		label,
		head,
		rows,
		corner = ''
	}: { label: string; head: string[]; rows: { name: string; cells: string[] }[]; corner?: string } = $props();
</script>

<details class="twin no-print">
	<summary>{label}</summary>
	<div class="scroll">
		<table>
			<thead>
				<tr><th>{corner}</th>{#each head as h (h)}<th>{h}</th>{/each}</tr>
			</thead>
			<tbody>
				{#each rows as r (r.name)}
					<tr><th scope="row">{r.name}</th>{#each r.cells as c, i (i)}<td>{c}</td>{/each}</tr>
				{/each}
			</tbody>
		</table>
	</div>
</details>

<style>
	.twin {
		margin-top: 0.9rem;
		font: 500 0.8rem var(--sans);
	}
	summary {
		cursor: pointer;
		color: var(--forest);
		width: fit-content;
	}
	.scroll {
		overflow-x: auto;
		margin-top: 0.6rem;
	}
	table {
		border-collapse: collapse;
		font-size: 0.72rem;
		white-space: nowrap;
	}
	th,
	td {
		padding: 0.25rem 0.5rem;
		text-align: right;
		border-bottom: 1px solid var(--rule);
	}
	thead th {
		font: 600 0.62rem var(--sans);
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
		border-bottom: 1.5px solid var(--ink);
	}
	tbody th {
		text-align: left;
		font-weight: 600;
		color: var(--ink);
		position: sticky;
		left: 0;
		background: var(--card);
	}
	td {
		color: var(--ink);
	}
</style>
