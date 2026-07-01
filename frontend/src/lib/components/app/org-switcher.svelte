<script lang="ts">
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
	import * as Sidebar from '$lib/components/ui/sidebar';
	import { useSidebar } from '$lib/components/ui/sidebar';
	import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down';
	import CheckIcon from '@lucide/svelte/icons/check';
	import BuildingIcon from '@lucide/svelte/icons/building-2';
	import { userStore, signIn, type Organization } from '$lib/user.svelte';

	const sidebar = useSidebar();
	const user = $derived(userStore.user);
	const orgs = $derived<Organization[]>(user ? (user.organizations ?? []) : []);
	const activeOrg = $derived(userStore.activeOrg);

	// Switching the active org while already signed in isn't a backend operation
	// yet (/auth/select-org only finishes a gated login), so picking a different
	// org re-runs login scoped to that org. Picking the current one is a no-op.
	function select(org: Organization) {
		if (org.id === activeOrg?.id) return;
		signIn();
	}
</script>

<Sidebar.Menu>
	<Sidebar.MenuItem>
		<DropdownMenu.Root>
			<DropdownMenu.Trigger>
				{#snippet child({ props })}
					<Sidebar.MenuButton
						{...props}
						size="lg"
						class="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
					>
						<div
							class="bg-sidebar-accent text-sidebar-accent-foreground border-sidebar-border flex aspect-square size-8 items-center justify-center rounded-md border"
						>
							<BuildingIcon class="size-4" />
						</div>
						<div class="grid flex-1 text-start text-sm leading-tight">
							<span class="truncate font-medium">{activeOrg?.name ?? 'No organization'}</span>
							{#if user?.role}
								<span class="text-muted-foreground truncate text-xs">{user.role}</span>
							{/if}
						</div>
						<ChevronsUpDownIcon class="ms-auto" />
					</Sidebar.MenuButton>
				{/snippet}
			</DropdownMenu.Trigger>
			<DropdownMenu.Content
				class="w-(--bits-dropdown-menu-anchor-width) min-w-56"
				align="start"
				side={sidebar.isMobile ? 'bottom' : 'right'}
				sideOffset={4}
			>
				<DropdownMenu.Label class="text-muted-foreground text-xs">Organizations</DropdownMenu.Label>
				{#each orgs as org (org.id)}
					<DropdownMenu.Item onSelect={() => select(org)} class="gap-2 p-2">
						<div class="flex size-6 items-center justify-center rounded-md border">
							<BuildingIcon class="size-3.5 shrink-0" />
						</div>
						<span class="flex-1 truncate">{org.name}</span>
						{#if org.id === activeOrg?.id}
							<CheckIcon class="size-4 shrink-0" />
						{/if}
					</DropdownMenu.Item>
				{/each}
			</DropdownMenu.Content>
		</DropdownMenu.Root>
	</Sidebar.MenuItem>
</Sidebar.Menu>
