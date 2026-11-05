<script lang="ts">
	// A note on the cover saying what the station runs on. A native <dialog>, so
	// Escape and the backdrop close it and focus stays inside; it is styled as a
	// bubble under the cover chips on a wide screen and a centred card on a phone.
	import type { Rig } from '../types';
	let { rig }: { rig: Rig } = $props();

	let dlg = $state<HTMLDialogElement | null>(null);
	let open = $state(false);

	function show() {
		open = true;
		dlg?.showModal();
	}
	function hide() {
		open = false;
		if (dlg?.open) dlg.close();
	}
</script>

<button class="chip" aria-expanded={open} onclick={show}>{rig.title}</button>

<dialog
	bind:this={dlg}
	onclose={() => (open = false)}
	onclick={(e) => e.target === dlg && hide()}
	aria-label={rig.title}
>
	<article>
		<button class="close" onclick={hide} aria-label="Close">×</button>
		<h2>{rig.title}</h2>
		{#if rig.lead}<p class="lead">{rig.lead}</p>{/if}
		{#if rig.specs?.length}
			<dl>
				{#each rig.specs as s, i (i)}
					<div class="row">
						<dt>{s.label ?? ''}</dt>
						<dd>{s.value}</dd>
					</div>
				{/each}
			</dl>
		{/if}
	</article>
</dialog>

<style>
	.chip {
		font: 600 0.7rem var(--sans);
		letter-spacing: 0.04em;
		color: rgba(248, 243, 232, 0.96);
		background: rgba(20, 26, 22, 0.45);
		padding: 0.3rem 0.7rem;
		border: 0;
		border-radius: 999px;
		cursor: pointer;
		backdrop-filter: blur(4px);
	}
	.chip:hover {
		background: rgba(20, 26, 22, 0.7);
	}

	dialog {
		border: 0;
		padding: 0;
		border-radius: 18px;
		background: var(--card);
		color: var(--ink);
		width: min(26rem, calc(100vw - 1.5rem));
		max-height: calc(100dvh - 2rem);
		box-shadow: 0 24px 60px -18px rgba(20, 26, 22, 0.55);
	}
	dialog:focus {
		outline: none;
	}
	dialog::backdrop {
		background: rgba(20, 26, 22, 0.5);
		backdrop-filter: blur(3px);
	}
	/* Wide screens: sit under the cover chips, so it reads as a bubble from the
	   button. Phones keep the dialog's own centring. */
	@media (min-width: 761px) {
		dialog {
			margin: 7.5rem 0 0 clamp(1.25rem, 6vw, 5rem);
		}
	}

	article {
		position: relative;
		padding: 1.3rem 1.4rem 1.5rem;
	}
	.close {
		position: absolute;
		right: 0.7rem;
		top: 0.55rem;
		width: 2rem;
		height: 2rem;
		display: grid;
		place-items: center;
		border: 0;
		border-radius: 50%;
		background: transparent;
		color: var(--muted);
		font-size: 1.4rem;
		line-height: 1;
		cursor: pointer;
	}
	.close:hover {
		background: var(--paper-deep);
		color: var(--ink);
	}
	h2 {
		margin: 0 2rem 0.5rem 0;
		font: 600 1.15rem var(--serif);
		letter-spacing: -0.01em;
	}
	.lead {
		margin: 0 0 1rem;
		font: 400 0.86rem/1.55 var(--sans);
		color: var(--muted);
	}
	dl {
		margin: 0;
		border-top: 1px solid var(--rule);
	}
	.row {
		display: grid;
		grid-template-columns: 7.5rem 1fr;
		gap: 0.6rem;
		padding: 0.5rem 0;
		border-bottom: 1px solid var(--rule);
	}
	dt {
		font: 600 0.66rem var(--sans);
		letter-spacing: 0.08em;
		text-transform: uppercase;
		color: var(--muted);
		padding-top: 0.12rem;
	}
	dd {
		margin: 0;
		font: 400 0.84rem/1.45 var(--sans);
	}
	@media (max-width: 420px) {
		.row {
			grid-template-columns: 1fr;
			gap: 0.1rem;
		}
	}
</style>
