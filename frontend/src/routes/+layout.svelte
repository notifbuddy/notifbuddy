<script lang="ts">
	import './layout.css';
	import favicon from '$lib/assets/favicon.svg';
	import AppShell from '$lib/components/app/app-shell.svelte';
	import { userStore } from '$lib/user.svelte';

	let { children } = $props();

	// Load the session once for the whole app. The shell (sidebar + content area)
	// is shown only when signed in; the signed-out / mid-login states render bare
	// so the dashboard's centered login card isn't wrapped in app chrome.
	userStore.load();
	const signedIn = $derived(!!userStore.user);
</script>

<svelte:head><link rel="icon" href={favicon} /></svelte:head>

{#if signedIn}
	<AppShell>
		{@render children()}
	</AppShell>
{:else}
	{@render children()}
{/if}
