<script lang="ts">
	import { cn } from '$lib/utils';
	import { page } from '$app/state';

	type Tab = { title: string; url: string };
	let { tabs }: { tabs: Tab[] } = $props();

	const path = $derived(page.url.pathname);
	const isActive = (url: string) => path === url || path.startsWith(url + '/');
</script>

<!-- Secondary in-page navigation: an underline tab row for a landing page's
     sub-sections (e.g. Integrations → Workspace | User). -->
<nav class="border-border flex gap-4 border-b">
	{#each tabs as tab (tab.url)}
		<a
			href={tab.url}
			class={cn(
				'-mb-px border-b-2 px-1 pb-2.5 text-sm font-medium transition-colors',
				isActive(tab.url)
					? 'border-primary text-foreground'
					: 'text-muted-foreground hover:text-foreground border-transparent'
			)}
		>
			{tab.title}
		</a>
	{/each}
</nav>
