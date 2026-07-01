<script lang="ts">
	import IntegrationLevel from '../integration-level.svelte';
	import PageTabs from '$lib/components/app/page-tabs.svelte';
	import { INTEGRATION_TABS } from '../tabs';
</script>

<div class="flex flex-col gap-6">
	<div class="flex flex-col gap-1">
		<h1 class="text-2xl font-semibold tracking-tight">Integrations</h1>
		<p class="text-muted-foreground text-sm">
			Personal connections used to act as you for two-way sync. These are yours alone.
		</p>
	</div>

	<PageTabs tabs={INTEGRATION_TABS} />

	<IntegrationLevel level="user" />
</div>
