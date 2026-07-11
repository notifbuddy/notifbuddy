<script lang="ts">
	// The shared body of each app's routes/+error.svelte. SvelteKit only
	// discovers route files inside an app's own routes/ directory, so those
	// files still exist per app — as thin shims over this component.
	import { page } from '$app/state';
	import ErrorPage from './error-page.svelte';

	let { privacyHref }: { privacyHref?: string } = $props();
</script>

<svelte:head>
	<title>{page.status === 404 ? 'Page not found' : 'Something went wrong'} — notifbuddy</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<ErrorPage status={page.status} {privacyHref} />
