<script lang="ts">
	// API browser-redirect failures (NOT-37). Path is /interrupted — SvelteKit
	// reserves /error for +error.svelte.
	import { page } from '$app/state';
	import SignalErrorBoard from '$lib/components/app/signal-error-board.svelte';
	import {
		ctaLabelFor,
		homeHrefFor,
		messageForCode,
		titleFor
	} from '$lib/browser-error';

	const params = $derived(page.url.searchParams);
	const status = $derived.by(() => {
		const statusRaw = Number(params.get('status') ?? '500');
		return Number.isFinite(statusRaw) && statusRaw >= 400 ? statusRaw : 500;
	});
	const code = $derived(params.get('code'));
	const provider = $derived(params.get('provider'));
	const title = $derived(titleFor(provider, code));
	const detail = $derived(messageForCode(code, provider));
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
