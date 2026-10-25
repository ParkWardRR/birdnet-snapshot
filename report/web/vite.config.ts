import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// The build lands in ../dist, which the Go binary embeds (report.go).
export default defineConfig({
	plugins: [
		sveltekit({
			compilerOptions: {
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			adapter: adapter({ pages: '../dist', assets: '../dist', strict: true }),
			// A fixed name keeps the build reproducible (CI checks ../dist is current).
			version: { name: 'field-notes' },
			// Only same-origin code; SvelteKit pins its inline bootstrap by hash.
			csp: {
				mode: 'hash',
				directives: {
					'default-src': ['self'],
					'script-src': ['self'],
					'style-src': ['self', 'unsafe-inline'],
					'img-src': ['self', 'data:'],
					'media-src': ['self'],
					'font-src': ['self', 'data:'],
					'connect-src': ['self'],
					'object-src': ['none'],
					'base-uri': ['none'],
					'form-action': ['none']
				}
			}
		})
	]
});
