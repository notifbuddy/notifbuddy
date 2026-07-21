<script lang="ts">
	import './layout.css';
	import favicon from '$lib/assets/favicon.svg';
	import { ModeWatcher } from 'mode-watcher';
	import AppShell from '$lib/components/app/app-shell.svelte';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import { userStore } from '$lib/user.svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';

	let { children } = $props();

	// Load the session once for the whole app. `user` is a tri-state:
	//   undefined → still checking (don't render the page yet)
	//   null      → signed out (render children bare: the centered login card)
	//   User      → signed in; the app shell additionally requires an
	//               organization — an org-less session (fresh sign-up mid
	//               create-org step) renders bare, full-page, like the rest of
	//               the login flow.
	// Collapsing undefined+null would render deep routes bare/full-width for a
	// frame before the shell mounts, causing a width flash on reload.
	userStore.load();
	const user = $derived(userStore.user);

	// Org-scoped routes need an active organization: bounce org-less sessions
	// that deep-link anywhere else back to the entry route, which shows the
	// create-organization step. /interrupted is a bare branded board (API redirects).
	$effect(() => {
		const path = page.url.pathname;
		if (user && !user.organizationId && path !== '/' && path !== '/interrupted') goto('/');
	});
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
	<!-- App-wide SEO/social defaults; pages override the title. The og image is
	     the shared brand card (assets/og-card.png), served from the marketing
	     domain so links unfurl the same everywhere. -->
	<meta
		name="description"
		content="notifbuddy moves notifications both ways between Linear, Plain and Slack."
	/>
	<meta property="og:site_name" content="notifbuddy" />
	<meta property="og:title" content="notifbuddy — all the noise, one signal" />
	<meta
		property="og:description"
		content="Two-way notification sync between Linear, Plain and Slack."
	/>
	<meta property="og:image" content="https://notifbuddy.com/og-card.png" />
	<meta property="og:image:width" content="1200" />
	<meta property="og:image:height" content="630" />
	<meta name="twitter:card" content="summary_large_image" />
</svelte:head>

<!-- Manages the `dark` class on <html> + persists the choice. Defaults to dark
     (the app's original single mode) until the user toggles. -->
<ModeWatcher defaultMode="dark" />

{#if page.url.pathname === '/interrupted'}
	<!-- Full-bleed Quiet error board (API OAuth failures). Never wrap in AppShell;
	     don't wait on session either — this page is safe signed-out.
	     Path is /interrupted not /error — SvelteKit reserves /error for +error.svelte. -->
	{@render children()}
{:else if user === undefined}
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
{:else if user && user.organizationId}
	<AppShell>
		{@render children()}
	</AppShell>
{:else}
	{@render children()}
{/if}
