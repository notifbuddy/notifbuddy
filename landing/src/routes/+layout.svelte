<script lang="ts">
	import './layout.css';
	import { ModeWatcher } from 'mode-watcher';

	let { children } = $props();
</script>

<svelte:head>
	<link rel="icon" href="/favicon-96.png" type="image/png" sizes="96x96" />
	<link rel="icon" href="/favicon.svg" type="image/svg+xml" />
	<link rel="icon" href="/favicon.ico" sizes="48x48" />
</svelte:head>

<!-- Manages the `dark` class on <html> + persists the visitor's choice.
     Defaults to the system preference (no defaultMode override). -->
<ModeWatcher />

{@render children()}
