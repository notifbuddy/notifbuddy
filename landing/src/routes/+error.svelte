<script lang="ts">
	// Kit-rendered errors: dev-server 404s (production unmatched paths are
	// served build/404.html by Cloudflare instead) and any load/render failure.
	import { page } from '$app/state';
	import ErrorPage from '$lib/components/error-page.svelte';
</script>

<svelte:head>
	<title>{page.status === 404 ? 'Page not found' : 'Something went wrong'} — notifbuddy</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<ErrorPage status={page.status} />
