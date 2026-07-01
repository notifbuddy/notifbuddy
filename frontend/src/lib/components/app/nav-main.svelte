<script lang="ts" module>
	import LayoutDashboardIcon from '@lucide/svelte/icons/layout-dashboard';
	import PlugIcon from '@lucide/svelte/icons/plug';

	type NavItem = {
		title: string;
		url: string;
		icon?: typeof LayoutDashboardIcon;
		// Optional sub-links. When present the item renders as a collapsible group.
		items?: { title: string; url: string }[];
	};

	// The left-hand navigation. Items with `items` expand to sub-links; URLs map to
	// real routes that render in the content area (inset). Dashboard is a
	// product-based group: one child per connected product (Linear now, GitHub
	// later). The parent only expands — its `url` is the child prefix, used to
	// highlight/auto-open the group when a product page is active.
	export const NAV_MAIN: NavItem[] = [
		{
			title: 'Dashboard',
			url: '/dashboard',
			icon: LayoutDashboardIcon,
			items: [{ title: 'Linear', url: '/dashboard/linear' }]
		},
		{
			title: 'Integrations',
			url: '/settings/integrations',
			icon: PlugIcon,
			items: [
				{ title: 'Workspace', url: '/settings/integrations/workspace' },
				{ title: 'User', url: '/settings/integrations/user' }
			]
		}
	];
</script>

<script lang="ts">
	import * as Sidebar from '$lib/components/ui/sidebar';
	import * as Collapsible from '$lib/components/ui/collapsible';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
	import { page } from '$app/state';

	let { items = NAV_MAIN }: { items?: NavItem[] } = $props();

	const path = $derived(page.url.pathname);
	const isActive = (url: string) => (url === '/' ? path === '/' : path.startsWith(url));
</script>

<Sidebar.Group>
	<Sidebar.GroupLabel>Platform</Sidebar.GroupLabel>
	<Sidebar.Menu>
		{#each items as item (item.title)}
			{#if item.items?.length}
				<Collapsible.Root open={isActive(item.url)} class="group/collapsible">
					{#snippet child({ props })}
						<Sidebar.MenuItem {...props}>
							<Collapsible.Trigger>
								{#snippet child({ props })}
									<Sidebar.MenuButton {...props} tooltipContent={item.title}>
										{#if item.icon}<item.icon />{/if}
										<span>{item.title}</span>
										<ChevronRightIcon
											class="ml-auto transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90"
										/>
									</Sidebar.MenuButton>
								{/snippet}
							</Collapsible.Trigger>
							<Collapsible.Content>
								<Sidebar.MenuSub>
									{#each item.items as sub (sub.title)}
										<Sidebar.MenuSubItem>
											<Sidebar.MenuSubButton isActive={path === sub.url}>
												{#snippet child({ props })}
													<a href={sub.url} {...props}>
														<span>{sub.title}</span>
													</a>
												{/snippet}
											</Sidebar.MenuSubButton>
										</Sidebar.MenuSubItem>
									{/each}
								</Sidebar.MenuSub>
							</Collapsible.Content>
						</Sidebar.MenuItem>
					{/snippet}
				</Collapsible.Root>
			{:else}
				<Sidebar.MenuItem>
					<Sidebar.MenuButton tooltipContent={item.title} isActive={isActive(item.url)}>
						{#snippet child({ props })}
							<a href={item.url} {...props}>
								{#if item.icon}<item.icon />{/if}
								<span>{item.title}</span>
							</a>
						{/snippet}
					</Sidebar.MenuButton>
				</Sidebar.MenuItem>
			{/if}
		{/each}
	</Sidebar.Menu>
</Sidebar.Group>
