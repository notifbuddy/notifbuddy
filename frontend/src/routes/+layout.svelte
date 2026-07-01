<script lang="ts">
	import './layout.css';
	import favicon from '$lib/assets/favicon.svg';
	import { ModeWatcher } from 'mode-watcher';
	import AppShell from '$lib/components/app/app-shell.svelte';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import { userStore } from '$lib/user.svelte';

	let { children } = $props();

	// Load the session once for the whole app. `user` is a tri-state:
	//   undefined → still checking (don't render the page yet)
	//   null      → signed out (render children bare: the centered login card)
	//   User      → signed in (wrap children in the app shell)
	// Collapsing undefined+null would render deep routes bare/full-width for a
	// frame before the shell mounts, causing a width flash on reload.
	userStore.load();
	const user = $derived(userStore.user);
</script>

<svelte:head><link rel="icon" href={favicon} /></svelte:head>

<!-- Manages the `dark` class on <html> + persists the choice. Defaults to dark
     (the app's original single mode) until the user toggles. -->
<ModeWatcher defaultMode="dark" />

{#if user === undefined}
	<!-- Still checking the session — show a shell-shaped skeleton (top bar +
	     content placeholder) so there's no un-shelled full-width flash on reload
	     and the wait reads as loading, consistent with the app's skeletons. -->
	<div class="flex min-h-svh flex-col">
		<header class="flex h-14 items-center gap-3 border-b px-4 sm:px-6">
			<Skeleton class="size-6 rounded-md" />
			<Skeleton class="h-5 w-32" />
			<Skeleton class="ms-auto size-8 rounded-full" />
		</header>
		<main class="mx-auto w-full max-w-6xl flex-1 px-4 py-6 sm:px-6">
			<div class="flex flex-col gap-6">
				<div class="flex flex-col gap-2">
					<Skeleton class="h-8 w-48" />
					<Skeleton class="h-4 w-72" />
				</div>
				<Skeleton class="h-64 w-full rounded-xl" />
			</div>
		</main>
	</div>
{:else if user}
	<AppShell>
		{@render children()}
	</AppShell>
{:else}
	{@render children()}
{/if}
