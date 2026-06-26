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
			adapter: adapter({ fallback: 'index.html' })
		})
	]
});
