<script lang="ts">
	import type { Species } from '../types';
	import { hour, num, pct } from '../format';
	import Sparkline from './Sparkline.svelte';
	let { s, rank, eager = false }: { s: Species; rank: number; eager?: boolean } = $props();
</script>

<article class="card">
	<figure>
		{#if s.photo}
			<img src={s.photo.large} alt={s.common} loading={eager ? 'eager' : 'lazy'} decoding="async" />
			{#if s.photo.author}<figcaption>{s.photo.author}</figcaption>{/if}
		{:else}
			<div class="nophoto">{s.common}</div>
		{/if}
		<span class="rank">{rank}</span>
	</figure>
	<div class="body">
		<header>
			<h3>{s.common}</h3>
			<p class="sci">{s.scientific}</p>
		</header>
		<div class="badges">
			{#if s.isNew}<span class="badge new">New</span>{/if}
			{#if s.rarity}<span class="badge" class:rare={s.rarity !== 'very common' && s.rarity !== 'common'}>{s.rarity}</span>{/if}
			{#if s.family}<span class="badge plain">{s.family}</span>{/if}
		</div>
		<p class="count"><strong>{num(s.count)}</strong> detections <span>· {pct(s.share)} of all</span></p>
		<Sparkline values={s.hourly} />
		<dl>
			<div><dt>Peak</dt><dd>{hour(s.peakHour)}</dd></div>
			<div><dt>Days heard</dt><dd>{s.daysHeard}</dd></div>
			<div><dt>Confidence</dt><dd>{pct(s.avgConfidence)}</dd></div>
		</dl>
	</div>
</article>

<style>
	.card {
		background: var(--card);
		border: 1px solid var(--rule);
		border-radius: 18px;
		overflow: hidden;
		display: flex;
		flex-direction: column;
		break-inside: avoid;
		box-shadow: 0 1px 0 rgba(31, 42, 36, 0.04), 0 10px 30px -18px rgba(31, 42, 36, 0.35);
	}
	figure {
		margin: 0;
		position: relative;
		aspect-ratio: 4 / 3;
		background: var(--paper-deep);
	}
	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		display: block;
	}
	figcaption {
		position: absolute;
		right: 0.5rem;
		bottom: 0.4rem;
		font: 500 0.62rem var(--sans);
		color: rgba(255, 255, 255, 0.88);
		text-shadow: 0 1px 2px rgba(0, 0, 0, 0.6);
	}
	.nophoto {
		height: 100%;
		display: grid;
		place-items: center;
		font: italic 600 1.2rem var(--serif);
		color: var(--muted);
	}
	.rank {
		position: absolute;
		left: 0.7rem;
		top: 0.6rem;
		font: 600 0.8rem var(--serif);
		background: rgba(244, 239, 228, 0.92);
		color: var(--ink);
		border-radius: 999px;
		width: 1.8rem;
		height: 1.8rem;
		display: grid;
		place-items: center;
	}
	.body {
		padding: 1rem 1.1rem 1.1rem;
		display: grid;
		gap: 0.6rem;
	}
	h3 {
		font: 600 1.3rem/1.15 var(--serif);
		margin: 0;
		color: var(--ink);
	}
	.sci {
		margin: 0.15rem 0 0;
		font: italic 400 0.9rem var(--serif);
		color: var(--muted);
	}
	.badges {
		display: flex;
		flex-wrap: wrap;
		gap: 0.35rem;
	}
	.badge {
		font: 600 0.62rem var(--sans);
		letter-spacing: 0.08em;
		text-transform: uppercase;
		padding: 0.28rem 0.55rem;
		border-radius: 999px;
		background: color-mix(in srgb, var(--forest) 12%, transparent);
		color: var(--forest);
	}
	.badge.new {
		background: var(--rust);
		color: #fff8ee;
	}
	.badge.rare {
		background: color-mix(in srgb, var(--rust) 16%, transparent);
		color: var(--rust);
	}
	.badge.plain {
		background: color-mix(in srgb, var(--ink) 6%, transparent);
		color: var(--muted);
	}
	.count {
		margin: 0;
		font: 400 0.9rem var(--sans);
		color: var(--muted);
	}
	.count strong {
		font: 600 1.45rem var(--serif);
		color: var(--ink);
		margin-right: 0.2rem;
	}
	.count span {
		white-space: nowrap;
	}
	dl {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		margin: 0;
		border-top: 1px solid var(--rule);
		padding-top: 0.6rem;
	}
	dt {
		font: 600 0.6rem var(--sans);
		letter-spacing: 0.1em;
		text-transform: uppercase;
		color: var(--muted);
	}
	dd {
		margin: 0.15rem 0 0;
		font: 500 0.9rem var(--serif);
		color: var(--ink);
	}
	/* Print: nine cards on one Letter page. */
	@media print {
		figure {
			aspect-ratio: 5 / 2;
		}
		.body {
			padding: 0.55rem 0.7rem 0.65rem;
			gap: 0.35rem;
		}
		h3 {
			font-size: 0.95rem;
		}
		dd {
			font-size: 0.8rem;
		}
		dt {
			font-size: 0.52rem;
		}
		.count {
			font-size: 0.78rem;
		}
		.sci {
			font-size: 0.78rem;
		}
		.badge.plain {
			display: none;
		}
		.count strong {
			font-size: 1rem;
		}
		.card {
			box-shadow: none;
			border-radius: 12px;
		}
		:global(.spark) {
			--spark-h: 14px;
		}
		.badges {
			display: none;
		}
		dl {
			padding-top: 0.35rem;
		}
	}
</style>
