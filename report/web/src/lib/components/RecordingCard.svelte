<script lang="ts" module>
	// One clip plays at a time across every card on the page.
	let playing: HTMLAudioElement | null = null;
</script>

<script lang="ts">
	import type { Recording, Photo } from '../types';
	import { pct, stamp } from '../format';
	let {
		r,
		photo,
		eager = false
	}: { r: Recording; photo?: Photo; eager?: boolean } = $props();

	let audio = $state<HTMLAudioElement>();
	let paused = $state(true);
	let progress = $state(0);
	let failed = $state(false);

	function toggle() {
		if (!audio) return;
		if (audio.paused) {
			if (playing && playing !== audio) playing.pause();
			playing = audio;
			audio.play().catch(() => (failed = true));
		} else {
			audio.pause();
		}
	}
	function seek(e: MouseEvent) {
		if (!audio || !audio.duration) return;
		const box = (e.currentTarget as HTMLElement).getBoundingClientRect();
		audio.currentTime = ((e.clientX - box.left) / box.width) * audio.duration;
		if (audio.paused) toggle();
	}
	const onTime = () => {
		if (audio?.duration) progress = audio.currentTime / audio.duration;
	};
</script>

<article class="rec" class:on={!paused}>
	<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
	<div class="spec" onclick={seek} title="Play from here">
		{#if r.spectrogram}
			<img src={r.spectrogram} alt="Spectrogram of the {r.common} recording" loading={eager ? 'eager' : 'lazy'} decoding="async" />
		{:else}
			<div class="nospec"></div>
		{/if}
		<span class="head" style="left: {progress * 100}%" class:show={progress > 0}></span>
	</div>
	<div class="row">
		<button class="play no-print" onclick={toggle} aria-label={paused ? `Play the ${r.common} recording` : 'Pause'}>
			{#if paused}
				<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M8 5.5v13l11-6.5z" /></svg>
			{:else}
				<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7 5h3.5v14H7zM13.5 5H17v14h-3.5z" /></svg>
			{/if}
		</button>
		{#if photo}<img class="face" src={photo.small} alt="" loading={eager ? 'eager' : 'lazy'} />{/if}
		<div class="meta">
			<strong>{r.common}</strong>
			<span>{stamp(r.time)} · {pct(r.confidence)} sure{#if r.models > 1}{' · '}{r.models} models agree{/if}</span>
		</div>
	</div>
	{#if failed}<p class="err no-print">This browser can't play the clip.</p>{/if}
	<audio
		bind:this={audio}
		src={r.audio}
		preload="none"
		onplay={() => (paused = false)}
		onpause={() => (paused = true)}
		onended={() => {
			paused = true;
			progress = 0;
		}}
		ontimeupdate={onTime}
	></audio>
</article>

<style>
	.rec {
		background: var(--card);
		border: 1px solid var(--rule);
		border-radius: 18px;
		overflow: hidden;
		break-inside: avoid;
		box-shadow: 0 1px 0 rgba(31, 42, 36, 0.04), 0 10px 30px -18px rgba(31, 42, 36, 0.35);
		transition: box-shadow 0.2s;
	}
	.rec.on {
		box-shadow: 0 0 0 2px var(--gold), 0 14px 34px -18px rgba(31, 42, 36, 0.5);
	}
	.spec {
		position: relative;
		aspect-ratio: 8 / 3;
		background: #0b0b14;
		cursor: pointer;
	}
	.spec img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		/* The top quarter is above what camera mics record (16 kHz): crop it. */
		object-position: bottom;
		display: block;
	}
	.nospec {
		height: 100%;
		background: linear-gradient(180deg, #14142a, #2a1840);
	}
	.head {
		position: absolute;
		top: 0;
		bottom: 0;
		width: 2px;
		background: var(--gold);
		box-shadow: 0 0 8px var(--gold);
		opacity: 0;
		transition: opacity 0.2s;
		pointer-events: none;
	}
	.head.show {
		opacity: 1;
	}
	.row {
		display: flex;
		align-items: center;
		gap: 0.7rem;
		padding: 0.8rem 1rem;
	}
	.play {
		flex: none;
		width: 2.6rem;
		height: 2.6rem;
		border-radius: 50%;
		border: 0;
		background: var(--forest);
		color: #fbf8f1;
		display: grid;
		place-items: center;
		cursor: pointer;
		box-shadow: 0 6px 16px -8px rgba(31, 42, 36, 0.7);
	}
	.play svg {
		width: 1.2rem;
		height: 1.2rem;
		fill: currentColor;
	}
	.on .play {
		background: var(--rust);
	}
	.face {
		width: 2.4rem;
		height: 2.4rem;
		border-radius: 50%;
		object-fit: cover;
		flex: none;
	}
	.meta {
		min-width: 0;
	}
	.meta strong {
		display: block;
		font: 600 1.05rem var(--serif);
		color: var(--ink);
	}
	.meta span {
		font-size: 0.78rem;
		color: var(--muted);
	}
	.err {
		margin: 0 1rem 0.8rem;
		font-size: 0.75rem;
		color: var(--rust);
	}
	@media print {
		.rec {
			box-shadow: none;
			border-radius: 12px;
		}
		.row {
			padding: 0.45rem 0.6rem;
		}
		.meta strong {
			font-size: 0.9rem;
		}
		.meta span {
			font-size: 0.68rem;
		}
	}
</style>
