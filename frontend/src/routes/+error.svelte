<!-- Shim: SvelteKit requires the route file here; the page lives in shared/.
     /privacy only exists on the marketing site, so link out to it. -->
<script lang="ts">
	import ErrorRoute from '$shared/components/error-route.svelte';
</script>

<ErrorRoute privacyHref="https://notifbuddy.com/privacy" />
