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

			// SPA mode: prerender nothing server-side, emit a single fallback
			// page that the client-side router takes over. The browser calls the
			// Go API directly (see src/lib/api/client.ts). See +layout.ts for the
			// prerender/ssr flags that make this a true SPA.
			adapter: adapter({ fallback: 'index.html' }),

			// Components shared with the landing site (repo-root shared/): the
			// brand mark and the "signal lost" error page. Tailwind scans the
			// directory via `@source` in routes/layout.css.
			alias: { $shared: '../shared' },

			// Fatal-error fallback (rendered when the app itself can't boot, so
			// it's self-contained HTML) — also shared with the landing site.
			files: { errorTemplate: '../shared/templates/error.html' }
		})
	],
	// Let the dev server serve files from shared/, which sits outside this
	// app's root (one level up = the repo root).
	server: { fs: { allow: ['..'] } }
});
