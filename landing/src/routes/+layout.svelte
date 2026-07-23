<script lang="ts">
	import './layout.css';
	import { ModeWatcher, mode } from 'mode-watcher';

	let { children } = $props();

	// Hex equivalents of --background (layout.css). Kept as literals because
	// <meta name="theme-color"> does not resolve CSS variables.
	const themeColor = { light: '#ffffff', dark: '#121113' } as const;
</script>

<!-- Favicons live in static/ at stable URLs — NOT imported through Vite, which
     would inline the small SVG as a data: URI that Google's favicon crawler
     ignores (that's a generic-globe SERP icon). The SVG carries its own
     prefers-color-scheme styles for browser tabs; the 96px PNG is the ≥48px
     raster Google wants (SERPs show one static icon on Google's own chip, so
     no theme variant applies there). /favicon.ico stays as the legacy
     fallback. -->
<svelte:head>
	<link rel="icon" href="/favicon.svg" type="image/svg+xml" />
	<link rel="icon" href="/favicon-96.png" type="image/png" sizes="96x96" />
	<!-- When the visitor forces light/dark, a non-media theme-color overrides
	     the prefers-color-scheme metas in app.html. We manage this ourselves —
	     ModeWatcher's themeColors FOUC path treats mode "system" as dark. -->
	{#if mode.current === 'light'}
		<meta name="theme-color" content={themeColor.light} />
	{:else if mode.current === 'dark'}
		<meta name="theme-color" content={themeColor.dark} />
	{/if}
</svelte:head>

<!-- Manages the `dark` class on <html> + persists the visitor's choice.
     Defaults to the system preference (no defaultMode override). -->
<ModeWatcher />

{@render children()}
