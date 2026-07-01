<script lang="ts">
	import { goto } from '$app/navigation';
	import { SiLinear } from '@icons-pack/svelte-simple-icons';
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
	<div class="flex flex-col gap-6">
		<div class="flex flex-col gap-1">
			<h1 class="flex items-center gap-2 text-2xl font-semibold tracking-tight">
				<SiLinear class="size-5" /> Linear
			</h1>
			<p class="text-muted-foreground text-sm">
				Control how Linear issues open Slack channels. Templates and conditions use GitHub Actions
				expression syntax, e.g. <code class="text-xs">${'{{'} linear.data.identifier {'}}'}</code>.
			</p>
		</div>

		<LinearSettings />
	</div>
{/if}
