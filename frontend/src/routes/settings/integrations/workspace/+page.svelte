<script lang="ts">
	import IntegrationLevel from '../integration-level.svelte';
</script>

<div class="flex flex-col gap-6">
	<div class="flex flex-col gap-1">
		<h1 class="text-2xl font-semibold tracking-tight">Workspace integrations</h1>
		<p class="text-muted-foreground text-sm">
			Org-wide connections (app install / bot) shared by everyone in your organization.
		</p>
	</div>

	<IntegrationLevel level="workspace" />
</div>
