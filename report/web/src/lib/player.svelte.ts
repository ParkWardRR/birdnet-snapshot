// One audio element for the whole page. Only one clip plays at a time, "Play
// all" can chain clips, and Safari (which lets a page play audio from an element
// a click has already started) keeps working across the queue.
import type { Recording } from './types';

class Player {
	src = $state('');
	playing = $state(false);
	progress = $state(0);
	failed = $state(false);
	queue = $state<Recording[]>([]);
	index = $state(-1);
	#audio: HTMLAudioElement | null = null;

	/** The clip a queue is playing, if any. */
	get current(): Recording | undefined {
		return this.index >= 0 ? this.queue[this.index] : undefined;
	}

	#element(): HTMLAudioElement | null {
		if (!this.#audio && typeof Audio !== 'undefined') {
			const a = new Audio();
			a.preload = 'none';
			a.addEventListener('play', () => (this.playing = true));
			a.addEventListener('pause', () => (this.playing = false));
			a.addEventListener('timeupdate', () => {
				if (a.duration) this.progress = a.currentTime / a.duration;
			});
			a.addEventListener('ended', () => this.#ended());
			a.addEventListener('error', () => {
				this.failed = true;
				this.playing = false;
			});
			this.#audio = a;
		}
		return this.#audio;
	}

	#load(src: string): HTMLAudioElement | null {
		const a = this.#element();
		if (!a) return null;
		if (this.src !== src) {
			this.src = src;
			this.progress = 0;
			this.failed = false;
			a.src = src;
		}
		return a;
	}

	#play(a: HTMLAudioElement) {
		a.play().catch(() => {
			this.failed = true;
			this.playing = false;
		});
	}

	#ended() {
		this.progress = 0;
		if (this.index >= 0 && this.index + 1 < this.queue.length) {
			this.index++;
			const a = this.#load(this.queue[this.index].audio);
			if (a) {
				a.currentTime = 0;
				this.#play(a);
			}
		} else {
			this.index = -1;
			this.queue = [];
		}
	}

	/** Play a clip, or pause it if it is the one playing. */
	toggle(src: string) {
		this.#stopQueue();
		const a = this.#load(src);
		if (!a) return;
		if (this.playing && this.src === src) a.pause();
		else this.#play(a);
	}

	/** Play a clip from a fraction of its length (a tap on its spectrogram). */
	seek(src: string, fraction: number) {
		this.#stopQueue();
		const a = this.#load(src);
		if (!a) return;
		const go = () => {
			if (a.duration) a.currentTime = fraction * a.duration;
			this.#play(a);
		};
		if (a.readyState >= 1) go();
		else {
			a.addEventListener('loadedmetadata', go, { once: true });
			a.load();
		}
	}

	/** Play clips one after another. */
	playAll(items: Recording[]) {
		if (!items.length) return;
		this.queue = items;
		this.index = 0;
		const a = this.#load(items[0].audio);
		if (a) {
			a.currentTime = 0;
			this.#play(a);
		}
	}

	stop() {
		this.#stopQueue();
		this.#audio?.pause();
		this.progress = 0;
	}

	#stopQueue() {
		this.index = -1;
		this.queue = [];
	}
}

export const player = new Player();
