<script lang="ts">
	// The link-preview image: the page at ?card=1, photographed at 1200 x 630.
	import type { Report } from '../types';
	import { num } from '../format';
	let { report }: { report: Report } = $props();
	const top = $derived(report.species[0]);
</script>

<div class="card">
	{#if top?.photo}<img class="bg" src={top.photo.large} alt="" loading="eager" />{/if}
	<div class="veil"></div>
	<div class="text">
		<p class="kicker">{report.station}{#if report.issue} · No. {report.issue.number}{/if}</p>
		<h1>{report.title}</h1>
		<p class="period">{report.period.label}</p>
	</div>
	<div class="stats">
		<div><span>{num(report.totals.detections)}</span>detections</div>
		<div><span>{report.totals.species}</span>species</div>
		<div><span>{report.totals.newSpecies}</span>new</div>
	</div>
</div>

<style>
	:global(html),
	:global(body) {
		margin: 0;
		background: #14201a;
	}
	.card {
		position: relative;
		width: 1200px;
		height: 630px;
		overflow: hidden;
		background: #14201a;
		color: #f8f3e8;
	}
	.bg {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		object-fit: cover;
		object-position: center 22%;
	}
	.veil {
		position: absolute;
		inset: 0;
		background: linear-gradient(180deg, rgba(20, 26, 22, 0) 25%, rgba(20, 26, 22, 0.6) 62%, rgba(20, 26, 22, 0.92) 100%);
	}
	.text {
		position: absolute;
		left: 64px;
		bottom: 52px;
	}
	.kicker {
		margin: 0 0 10px;
		font: 600 17px var(--sans);
		letter-spacing: 0.22em;
		text-transform: uppercase;
		opacity: 0.88;
	}
	h1 {
		margin: 0;
		font: italic 600 124px / 0.95 var(--serif);
		letter-spacing: -0.02em;
		font-variation-settings: 'SOFT' 100, 'WONK' 1;
	}
	.period {
		margin: 14px 0 0;
		font: 600 22px var(--sans);
		letter-spacing: 0.12em;
		text-transform: uppercase;
		color: var(--gold);
	}
	.stats {
		position: absolute;
		right: 64px;
		bottom: 56px;
		display: flex;
		gap: 40px;
		text-align: right;
		font: 600 15px var(--sans);
		letter-spacing: 0.12em;
		text-transform: uppercase;
		opacity: 0.95;
	}
	.stats span {
		display: block;
		font: 600 54px / 1 var(--serif);
		letter-spacing: 0;
		text-transform: none;
		margin-bottom: 6px;
	}
</style>
