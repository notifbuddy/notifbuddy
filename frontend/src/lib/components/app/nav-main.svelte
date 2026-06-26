<script lang="ts" module>
	import LayoutDashboardIcon from '@lucide/svelte/icons/layout-dashboard';
	import PlugIcon from '@lucide/svelte/icons/plug';

	type NavSubItem = { title: string; url: string };
	type NavItem = {
		title: string;
		url: string;
		icon?: typeof LayoutDashboardIcon;
		items?: NavSubItem[];
	};

	// The left-hand navigation. Direct links have no `items`; groups expand to
	// sub-links. URLs map to real routes that render in the content area (inset).
	export const NAV_MAIN: NavItem[] = [
		{ title: 'Dashboard', url: '/', icon: LayoutDashboardIcon },
		{
			title: 'Integrations',
			url: '/settings/integrations',
			icon: PlugIcon,
			items: [
				{ title: 'Connections', url: '/settings/integrations' },
				{ title: 'GitHub webhooks', url: '/settings/integrations/webhooks' }
			]
		}
	];
</script>

<script lang="ts">
	import * as Collapsible from '$lib/components/ui/collapsible';
	import * as Sidebar from '$lib/components/ui/sidebar';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
	import { page } from '$app/state';

	let { items = NAV_MAIN }: { items?: NavItem[] } = $props();

	const path = $derived(page.url.pathname);
	const isActive = (url: string) => (url === '/' ? path === '/' : path.startsWith(url));
	const groupActive = (item: NavItem) =>
		isActive(item.url) || (item.items?.some((s) => isActive(s.url)) ?? false);
</script>

<Sidebar.Group>
	<Sidebar.GroupLabel>Platform</Sidebar.GroupLabel>
	<Sidebar.Menu>
		{#each items as item (item.title)}
			{#if !item.items}
				<!-- Direct link. -->
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
			{:else}
				<!-- Collapsible group with sub-links. -->
				<Collapsible.Root open={groupActive(item)} class="group/collapsible">
					{#snippet child({ props })}
						<Sidebar.MenuItem {...props}>
							<Collapsible.Trigger>
								{#snippet child({ props })}
									<Sidebar.MenuButton
										{...props}
										tooltipContent={item.title}
										isActive={groupActive(item)}
									>
										{#if item.icon}<item.icon />{/if}
										<span>{item.title}</span>
										<ChevronRightIcon
											class="ms-auto transition-transform duration-200 group-data-[state=open]/collapsible:rotate-90"
										/>
									</Sidebar.MenuButton>
								{/snippet}
							</Collapsible.Trigger>
							<Collapsible.Content>
								<Sidebar.MenuSub>
									{#each item.items ?? [] as subItem (subItem.title)}
										<Sidebar.MenuSubItem>
											<Sidebar.MenuSubButton isActive={isActive(subItem.url)}>
												{#snippet child({ props })}
													<a href={subItem.url} {...props}>
														<span>{subItem.title}</span>
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
			{/if}
		{/each}
	</Sidebar.Menu>
</Sidebar.Group>
