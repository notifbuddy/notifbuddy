<script lang="ts">
	import * as Sidebar from '$lib/components/ui/sidebar';
	import * as Breadcrumb from '$lib/components/ui/breadcrumb';
	import { Separator } from '$lib/components/ui/separator';
	import AppSidebar from './app-sidebar.svelte';
	import { page } from '$app/state';
	import type { Snippet } from 'svelte';

	let { children }: { children: Snippet } = $props();

	// Map known routes to readable breadcrumb trails. Each entry is the list of
	// crumbs shown in the header for that path.
	const CRUMBS: { match: (p: string) => boolean; trail: string[] }[] = [
		{ match: (p) => p.startsWith('/dashboard/linear'), trail: ['Dashboard', 'Linear'] },
		{ match: (p) => p === '/' || p.startsWith('/dashboard'), trail: ['Dashboard'] },
		{
			match: (p) => p === '/settings/integrations/webhooks',
			trail: ['Integrations', 'GitHub webhooks']
		},
		{
			match: (p) => p === '/settings/integrations/linear-webhooks',
			trail: ['Integrations', 'Linear webhooks']
		},
		{
			match: (p) => p.startsWith('/settings/integrations/workspace'),
			trail: ['Integrations', 'Workspace']
		},
		{
			match: (p) => p.startsWith('/settings/integrations/user'),
			trail: ['Integrations', 'User']
		},
		{ match: (p) => p.startsWith('/settings/integrations'), trail: ['Integrations'] },
		{ match: (p) => p.startsWith('/settings/profile'), trail: ['Profile'] },
		{ match: (p) => p.startsWith('/settings/organization'), trail: ['Organization'] }
	];

	const trail = $derived(
		CRUMBS.find((c) => c.match(page.url.pathname))?.trail ?? ['Dashboard']
	);
</script>

<Sidebar.Provider>
	<AppSidebar />
	<Sidebar.Inset>
		<header
			class="flex h-14 shrink-0 items-center gap-2 border-b transition-[width,height] ease-linear group-has-data-[collapsible=icon]/sidebar-wrapper:h-12"
		>
			<div class="flex items-center gap-2 px-4">
				<Sidebar.Trigger class="-ms-1" />
				<Separator orientation="vertical" class="me-2 data-[orientation=vertical]:h-4" />
				<Breadcrumb.Root>
					<Breadcrumb.List>
						{#each trail as crumb, i (crumb)}
							<Breadcrumb.Item>
								{#if i === trail.length - 1}
									<Breadcrumb.Page>{crumb}</Breadcrumb.Page>
								{:else}
									<Breadcrumb.Page class="text-muted-foreground">{crumb}</Breadcrumb.Page>
								{/if}
							</Breadcrumb.Item>
							{#if i < trail.length - 1}
								<Breadcrumb.Separator />
							{/if}
						{/each}
					</Breadcrumb.List>
				</Breadcrumb.Root>
			</div>
		</header>
		<div class="flex flex-1 flex-col gap-4 p-4">
			{@render children()}
		</div>
	</Sidebar.Inset>
</Sidebar.Provider>
