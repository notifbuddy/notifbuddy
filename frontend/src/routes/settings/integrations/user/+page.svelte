<script lang="ts">
	import IntegrationLevel from '../integration-level.svelte';
</script>

<div class="flex flex-col gap-6">
	<div class="flex flex-col gap-1">
		<h1 class="text-2xl font-semibold tracking-tight">Your integrations</h1>
		<p class="text-muted-foreground text-sm">
			Personal connections used to act as you for two-way sync. These are yours alone.
		</p>
	</div>

	<IntegrationLevel level="user" />
</div>
