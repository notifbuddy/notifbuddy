<script lang="ts">
	// API browser-redirect failures (NOT-37). Path is /interrupted — SvelteKit
	// reserves /error for +error.svelte. Title/message come from the backend.
	import { page } from '$app/state';
	import SignalErrorBoard from '$lib/components/app/signal-error-board.svelte';
	import { ctaLabelFor, homeHrefFor } from '$lib/browser-error';

	const params = $derived(page.url.searchParams);
	const status = $derived.by(() => {
		const statusRaw = Number(params.get('status') ?? '500');
		return Number.isFinite(statusRaw) && statusRaw >= 400 ? statusRaw : 500;
	});
	const code = $derived(params.get('code'));
	const provider = $derived(params.get('provider'));
	const title = $derived(params.get('title')?.trim() || 'Unable to connect');
	const detail = $derived(
		params.get('message')?.trim() ||
			'Something interrupted the connection. Give it a moment and try again.'
	);
	const homeHref = $derived(homeHrefFor(code, provider));
	const ctaLabel = $derived(ctaLabelFor(code, provider));
</script>

<svelte:head>
	<title>{title} — notifbuddy</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<SignalErrorBoard
	{status}
	{code}
	{title}
	{detail}
	{homeHref}
	{ctaLabel}
/>
