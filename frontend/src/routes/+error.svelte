<!-- Shim: SvelteKit requires the route file here. Dashboard kit errors use the
     same Quiet Empty/Button/Badge board as /interrupted. -->
<script lang="ts">
	import { page } from '$app/state';
	import SignalErrorBoard from '$lib/components/app/signal-error-board.svelte';

	const notFound = $derived(page.status === 404);
	const title = $derived(notFound ? 'Unable to find this page' : 'Unable to load this page');
	const detail = $derived(
		notFound
			? "This page isn't syncing here — it may have moved, or it never existed. Nothing gets lost for long, though."
			: 'An unexpected error interrupted the signal. Give it a moment and try again.'
	);
</script>

<svelte:head>
	<title>{notFound ? 'Page not found' : 'Something went wrong'} — notifbuddy</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<SignalErrorBoard
	status={page.status}
	{title}
	{detail}
	privacyHref="https://notifbuddy.com/privacy"
/>
