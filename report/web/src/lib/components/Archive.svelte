<script lang="ts">
	// The archive: every weekly issue, newest first.
	import type { IssueIndex } from '../types';
	import { day, num, stamp } from '../format';
	let { idx, title }: { idx: IssueIndex; title: string } = $props();
</script>

<main class="archive">
	<nav class="nav"><a href="./">← Latest</a><a href="feed.xml">Feed</a></nav>
	<h1>{title} <em>archive</em></h1>
	<p class="dek">
		Each week's notes, as they stood when the week ended. {idx.issues.length}
		{idx.issues.length === 1 ? 'issue' : 'issues'} so far.
	</p>
	{#if idx.issues.length}
		<ol class="issues">
			{#each idx.issues as e (e.id)}
				<li>
					<a class="issue" href="?issue={e.id}">
						{#if e.topPhoto}<img src={e.topPhoto} alt="" loading="lazy" />{:else}<span class="ph"></span>{/if}
						<div class="body">
							<p class="no">No. {e.number}</p>
							<h2>{e.label}</h2>
							<p class="stats">{num(e.detections)} detections · {e.species} species{#if e.top} · led by {e.top}{/if}</p>
							{#if e.summary}<p class="sum">{e.summary}</p>{/if}
							<p class="pub">Published {stamp(e.published)}</p>
						</div>
					</a>
					{#if e.pdf}<a class="pdf" href={e.pdf} download="field-notes-{e.end}.pdf">PDF</a>{/if}
				</li>
			{/each}
		</ol>
	{:else}
		<p class="none">The first issue appears when the first week ends.</p>
	{/if}
</main>

<style>
	.archive {
		max-width: 52rem;
		margin: 0 auto;
		padding: clamp(1.5rem, 5vw, 3.5rem) clamp(1.25rem, 4vw, 2.5rem) 4rem;
	}
	.nav {
		display: flex;
		gap: 1.2rem;
		font: 600 0.78rem var(--sans);
		letter-spacing: 0.04em;
		margin-bottom: 2rem;
	}
	.nav a {
		color: var(--forest);
	}
	h1 {
		font: 600 clamp(2.4rem, 7vw, 4.2rem) / 1 var(--serif);
		margin: 0 0 0.6rem;
		letter-spacing: -0.02em;
	}
	h1 em {
		color: var(--rust);
		font-weight: 500;
	}
	.dek {
		font: 400 1.05rem/1.55 var(--serif);
		color: var(--muted);
		margin: 0 0 2rem;
	}
	.issues {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		gap: 1rem;
	}
	li {
		position: relative;
	}
	.issue {
		display: grid;
		grid-template-columns: 8.5rem 1fr;
		gap: 1.2rem;
		background: var(--card);
		border: 1px solid var(--rule);
		border-radius: 20px;
		overflow: hidden;
		color: inherit;
		text-decoration: none;
		transition: transform 0.15s;
	}
	.issue:hover {
		transform: translateY(-2px);
	}
	img,
	.ph {
		width: 100%;
		height: 100%;
		min-height: 8rem;
		object-fit: cover;
		display: block;
		background: var(--paper-deep);
	}
	.body {
		padding: 1rem 3.4rem 1rem 0;
	}
	.no {
		margin: 0;
		font: 600 0.66rem var(--sans);
		letter-spacing: 0.14em;
		text-transform: uppercase;
		color: var(--rust);
	}
	h2 {
		font: 600 1.45rem/1.15 var(--serif);
		margin: 0.15rem 0 0.35rem;
	}
	.stats {
		margin: 0;
		font: 500 0.82rem var(--sans);
		color: var(--forest);
	}
	.sum {
		margin: 0.45rem 0 0;
		font: 400 0.92rem/1.45 var(--serif);
		color: var(--muted);
		display: -webkit-box;
		-webkit-line-clamp: 3;
		line-clamp: 3;
		-webkit-box-orient: vertical;
		overflow: hidden;
	}
	.pub {
		margin: 0.5rem 0 0;
		font: 500 0.7rem var(--sans);
		color: var(--muted);
	}
	.pdf {
		position: absolute;
		right: 1rem;
		top: 1rem;
		font: 600 0.7rem var(--sans);
		letter-spacing: 0.06em;
		padding: 0.3rem 0.7rem;
		border-radius: 999px;
		background: var(--forest);
		color: #fbf8f1;
		text-decoration: none;
	}
	.none {
		color: var(--muted);
		font: italic 400 1.1rem var(--serif);
	}
	@media (max-width: 560px) {
		.issue {
			grid-template-columns: 1fr;
		}
		img,
		.ph {
			min-height: 0;
			height: 9rem;
		}
		.body {
			padding: 0 1rem 1rem;
		}
		.pdf {
			top: 0.7rem;
		}
	}
</style>
