<script lang="ts">
	import './layout.css';
	import { ModeWatcher } from 'mode-watcher';

	let { children } = $props();
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
</svelte:head>

<!-- Manages the `dark` class on <html> + persists the visitor's choice.
     Defaults to the system preference (no defaultMode override). -->
<ModeWatcher />

{@render children()}
