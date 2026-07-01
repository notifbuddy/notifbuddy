<script lang="ts">
	import { goto } from '$app/navigation';
	import { userStore } from '$lib/user.svelte';
	import LinearSettings from '$lib/components/app/linear-settings.svelte';
	import PageTabs from '$lib/components/app/page-tabs.svelte';
	import { DASHBOARD_TABS } from '../tabs';

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
			<h1 class="text-2xl font-semibold tracking-tight">Dashboard</h1>
			<p class="text-muted-foreground text-sm">
				Control how your synced products open Slack channels.
			</p>
		</div>

		<PageTabs tabs={DASHBOARD_TABS} />

		<LinearSettings />
	</div>
{/if}
