<script lang="ts">
	import ErrorPage from '$lib/components/error-page.svelte';
</script>

<svelte:head>
	<title>Page not found — notifbuddy</title>
	<!-- The route itself answers 200 at /404; only the Cloudflare-served 404
	     responses matter, so keep the page out of search indexes. -->
	<meta name="robots" content="noindex" />
</svelte:head>

<ErrorPage status={404} />
