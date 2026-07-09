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
			// content. The only runtime call is the waitlist form's POST to the
			// standalone waitlist service.
			adapter: adapter()
		})
	]
});
