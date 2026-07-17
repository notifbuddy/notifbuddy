import tailwindcss from '@tailwindcss/vite';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [
		tailwindcss(),
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) => filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},

			// Marketing site: everything is prerendered to plain HTML (no
			// fallback SPA shell) so crawlers and link unfurlers get real
			// content.
			adapter: adapter(),

			// Components shared with the dashboard app (repo-root shared/): the
			// brand mark and the "signal lost" error page. Tailwind scans the
			// directory via `@source` in routes/layout.css.
			alias: { $shared: '../shared' },

			// Fatal-error fallback (rendered when the app itself can't boot, so
			// it's self-contained HTML) — also shared with the dashboard app.
			files: { errorTemplate: '../shared/templates/error.html' }
		})
	],
	// Let the dev server serve files from shared/, which sits outside this
	// app's root (one level up = the repo root).
	server: { fs: { allow: ['..'] } }
});
