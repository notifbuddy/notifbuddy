<script lang="ts">
	import TopNav from './top-nav.svelte';
	import type { Snippet } from 'svelte';

	let { children }: { children: Snippet } = $props();
</script>

<!-- Top-nav shell: a sticky header with the org switcher, primary nav, and the
     profile menu; page content renders full-view inside a centered container.
     Sub-pages surface as secondary tabs on their landing page, not in the bar. -->
<div class="flex min-h-svh flex-col">
	<TopNav />
	<main class="mx-auto w-full max-w-6xl flex-1 px-4 py-6 sm:px-6">
		{@render children()}
	</main>
</div>
