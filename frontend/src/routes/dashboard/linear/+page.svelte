<script lang="ts">
	import { goto } from '$app/navigation';
	import { userStore } from '$lib/user.svelte';
	import LinearSettings from '$lib/components/app/linear-settings.svelte';

	// Auth state: undefined = still checking, null = signed out, else User.
	const user = $derived(userStore.user);

	// This page lives under the app shell (signed-in only). If the session
	// resolves to signed-out, bounce to the entry route to sign in.
	$effect(() => {
		if (user === null) goto('/');
	});
</script>

{#if user}
	<div class="w-full">
		<LinearSettings />
	</div>
{/if}
