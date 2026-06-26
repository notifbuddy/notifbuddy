<script lang="ts" module>
	import LayoutDashboardIcon from '@lucide/svelte/icons/layout-dashboard';
	import PlugIcon from '@lucide/svelte/icons/plug';

	type NavItem = {
		title: string;
		url: string;
		icon?: typeof LayoutDashboardIcon;
	};

	// The left-hand navigation. Direct links have no `items`; groups expand to
	// sub-links. URLs map to real routes that render in the content area (inset).
	export const NAV_MAIN: NavItem[] = [
		{ title: 'Dashboard', url: '/', icon: LayoutDashboardIcon },
		{ title: 'Integrations', url: '/settings/integrations', icon: PlugIcon }
	];
</script>

<script lang="ts">
	import * as Sidebar from '$lib/components/ui/sidebar';
	import { page } from '$app/state';

	let { items = NAV_MAIN }: { items?: NavItem[] } = $props();

	const path = $derived(page.url.pathname);
	const isActive = (url: string) => (url === '/' ? path === '/' : path.startsWith(url));
</script>

<Sidebar.Group>
	<Sidebar.GroupLabel>Platform</Sidebar.GroupLabel>
	<Sidebar.Menu>
		{#each items as item (item.title)}
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
		{/each}
	</Sidebar.Menu>
</Sidebar.Group>
