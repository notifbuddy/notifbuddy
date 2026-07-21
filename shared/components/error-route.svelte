<script lang="ts">
	// Shared body of landing's routes/+error.svelte. Dashboard uses its own
	// SignalErrorBoard (shadcn Empty/Button/Badge) instead.
	import { page } from '$app/state';
	import ErrorPage from './error-page.svelte';

	let { privacyHref }: { privacyHref?: string } = $props();
</script>

<svelte:head>
	<title>{page.status === 404 ? 'Page not found' : 'Something went wrong'} — notifbuddy</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<ErrorPage status={page.status} {privacyHref} />
