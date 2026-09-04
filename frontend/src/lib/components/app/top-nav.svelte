<script lang="ts" module>
	import LayoutDashboardIcon from '@lucide/svelte/icons/layout-dashboard';
	import PlugIcon from '@lucide/svelte/icons/plug';

	type NavLink = { title: string; url: string; icon: typeof LayoutDashboardIcon };

	// Primary top-level destinations. Sub-pages live as secondary tabs on each
	// landing page, not in this bar.
	export const NAV_LINKS: NavLink[] = [
		{ title: 'Dashboard', url: '/dashboard', icon: LayoutDashboardIcon },
		{ title: 'Integrations', url: '/settings/integrations', icon: PlugIcon }
	];
</script>

<script lang="ts">
	import * as Avatar from '$lib/components/ui/avatar';
	import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
	import { Button } from '$lib/components/ui/button';
	import Logo from '$shared/components/logo.svelte';
	import { cn } from '$lib/utils';
	import BookOpenIcon from '@lucide/svelte/icons/book-open';
	import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down';
	import CheckIcon from '@lucide/svelte/icons/check';
	import BuildingIcon from '@lucide/svelte/icons/building-2';
	import UserIcon from '@lucide/svelte/icons/user';
	import CreditCardIcon from '@lucide/svelte/icons/credit-card';
	import LogOutIcon from '@lucide/svelte/icons/log-out';
	import { page } from '$app/state';
	import {
		userStore,
		switchOrg,
		signOut,
		displayName,
		initials,
		avatarUrl,
		type Organization
	} from '$lib/user.svelte';
	import { orgProfileStore } from '$lib/org-profile.svelte';
	import MarbleAvatar from './marble-avatar.svelte';

	const user = $derived(userStore.user);
	const activeOrg = $derived(userStore.activeOrg);
	const orgs = $derived<Organization[]>(user ? (user.organizations ?? []) : []);

	// The active org's avatar (uploaded image or generated marble) for the
	// switcher; falls back to the building icon until the profile loads.
	const orgProfile = $derived(orgProfileStore.profile);
	$effect(() => {
		if (activeOrg?.id) orgProfileStore.load(activeOrg.id);
	});

	const path = $derived(page.url.pathname);
	const isActive = (url: string) => path === url || path.startsWith(url + '/');

	// Switching org sets the session's active organization in authd and
	// reloads. Picking the current one is a no-op.
	function selectOrg(org: Organization) {
		if (org.id === activeOrg?.id) return;
		switchOrg(org.id);
	}
</script>

<header
	class="bg-background/95 supports-[backdrop-filter]:bg-background/80 sticky top-0 z-40 border-b backdrop-blur"
>
	<div class="mx-auto flex h-14 w-full max-w-6xl items-center gap-2 px-4 sm:px-6">
		<!-- Full brand lockup: mark + wordmark, links home. -->
		<a
			href="/dashboard"
			aria-label="notifbuddy — dashboard"
			class="text-foreground focus-visible:ring-ring flex items-center rounded-md outline-none focus-visible:ring-2"
		>
			<!-- Below sm the full lockup + switcher + nav can't share the row, so
			     the mark stands alone (same convention as the marketing header). -->
			<span class="hidden sm:block"><Logo size={26} /></span>
			<span class="sm:hidden"><Logo size={26} wordmark={false} /></span>
		</a>

		<div class="bg-border mx-1 hidden h-5 w-px sm:block" aria-hidden="true"></div>

		<!-- Org switcher -->
		<DropdownMenu.Root>
			<DropdownMenu.Trigger>
				{#snippet child({ props })}
					<Button {...props} variant="ghost" class="gap-2 px-2">
						{#if orgProfile?.avatarUrl}
							<img src={orgProfile.avatarUrl} alt="" class="size-5 shrink-0 rounded-full object-cover" />
						{:else if orgProfile?.avatarSeed}
							<MarbleAvatar seed={orgProfile.avatarSeed} class="size-5 shrink-0" />
						{:else}
							<div
								class="bg-muted text-muted-foreground border-border flex size-5 items-center justify-center rounded-full border"
							>
								<BuildingIcon class="size-3" />
							</div>
						{/if}
						<span class="max-w-24 truncate font-medium sm:max-w-40">
							{activeOrg?.name ?? 'No organization'}
						</span>
						<ChevronsUpDownIcon class="text-muted-foreground size-4" />
					</Button>
				{/snippet}
			</DropdownMenu.Trigger>
			<DropdownMenu.Content class="min-w-56" align="start" sideOffset={6}>
				<DropdownMenu.Label class="text-muted-foreground text-xs">Organizations</DropdownMenu.Label>
				{#each orgs as org (org.id)}
					<DropdownMenu.Item onSelect={() => selectOrg(org)} class="gap-2 p-2">
						{#if org.id === activeOrg?.id && orgProfile?.avatarUrl}
							<img src={orgProfile.avatarUrl} alt="" class="size-6 shrink-0 rounded-full object-cover" />
						{:else if org.id === activeOrg?.id && orgProfile?.avatarSeed}
							<MarbleAvatar seed={orgProfile.avatarSeed} class="size-6 shrink-0" />
						{:else}
							<div class="flex size-6 items-center justify-center rounded-full border">
								<BuildingIcon class="size-3.5 shrink-0" />
							</div>
						{/if}
						<span class="flex-1 truncate">{org.name}</span>
						{#if org.id === activeOrg?.id}
							<CheckIcon class="size-4 shrink-0" />
						{/if}
					</DropdownMenu.Item>
				{/each}
			</DropdownMenu.Content>
		</DropdownMenu.Root>

		<div class="bg-border mx-1 hidden h-5 w-px sm:block" aria-hidden="true"></div>

		<!-- Primary nav: icon-only below sm, where the labels don't fit. -->
		<nav class="flex items-center gap-1">
			{#each NAV_LINKS as link (link.url)}
				<Button
					href={link.url}
					variant="ghost"
					size="sm"
					aria-label={link.title}
					class={cn('gap-2', isActive(link.url) ? 'text-foreground' : 'text-muted-foreground')}
				>
					<link.icon class="size-4" />
					<span class="hidden sm:inline">{link.title}</span>
				</Button>
			{/each}
		</nav>

		<!-- Right cluster: external docs link | divider | profile menu. gap-2 +
		     the divider's mx-1 = 12px a side, the same rhythm as the dividers
		     on the left rail. -->
		<div class="ms-auto flex items-center gap-2">
		<!-- Docs moves into the profile menu below sm (see the sm:hidden item). -->
		<Button
			href="https://docs.notifbuddy.com"
			target="_blank"
			rel="noopener"
			variant="ghost"
			size="sm"
			class="text-muted-foreground hidden gap-2 sm:inline-flex"
		>
			<BookOpenIcon class="size-4" />
			Docs
		</Button>

		<div class="bg-border mx-1 mr-2 hidden h-5 w-px sm:block" aria-hidden="true"></div>

		<!-- Profile menu -->
		{#if user}
			<DropdownMenu.Root>
				<DropdownMenu.Trigger>
					{#snippet child({ props })}
						<Button {...props} variant="ghost" size="icon" class="rounded-full">
							<Avatar.Root class="size-8">
								<Avatar.Image src={avatarUrl(user)} alt={displayName(user)} />
								<Avatar.Fallback class="bg-muted text-muted-foreground text-xs font-medium">
									{initials(user)}
								</Avatar.Fallback>
							</Avatar.Root>
						</Button>
					{/snippet}
				</DropdownMenu.Trigger>
				<DropdownMenu.Content class="min-w-56" align="end" sideOffset={6}>
					<DropdownMenu.Label class="p-0 font-normal">
						<div class="flex items-center gap-2 px-1 py-1.5 text-start text-sm">
							<Avatar.Root class="size-8">
								<Avatar.Image src={avatarUrl(user)} alt={displayName(user)} />
								<Avatar.Fallback class="bg-muted text-muted-foreground text-xs font-medium">
									{initials(user)}
								</Avatar.Fallback>
							</Avatar.Root>
							<div class="grid flex-1 text-start text-sm leading-tight">
								<span class="truncate font-medium">{displayName(user)}</span>
								<span class="text-muted-foreground truncate text-xs">{user.email}</span>
							</div>
						</div>
					</DropdownMenu.Label>
					<DropdownMenu.Separator />
					<DropdownMenu.Group>
						<DropdownMenu.Item>
							{#snippet child({ props })}
								<a href="/settings/profile" {...props}>
									<UserIcon />
									Profile
								</a>
							{/snippet}
						</DropdownMenu.Item>
						<DropdownMenu.Item>
							{#snippet child({ props })}
								<a href="/settings/organization" {...props}>
									<BuildingIcon />
									Organization
								</a>
							{/snippet}
						</DropdownMenu.Item>
						<DropdownMenu.Item>
							{#snippet child({ props })}
								<a href="/settings/billing" {...props}>
									<CreditCardIcon />
									Billing
								</a>
							{/snippet}
						</DropdownMenu.Item>
						<DropdownMenu.Item class="sm:hidden">
							{#snippet child({ props })}
								<a href="https://docs.notifbuddy.com" target="_blank" rel="noopener" {...props}>
									<BookOpenIcon />
									Docs
								</a>
							{/snippet}
						</DropdownMenu.Item>
					</DropdownMenu.Group>
					<DropdownMenu.Separator />
					<DropdownMenu.Item variant="destructive" onSelect={signOut}>
						<LogOutIcon />
						Log out
					</DropdownMenu.Item>
				</DropdownMenu.Content>
			</DropdownMenu.Root>
		{/if}
		</div>
	</div>
</header>
