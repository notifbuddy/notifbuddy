<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as Avatar from '$lib/components/ui/avatar';
	import * as ToggleGroup from '$lib/components/ui/toggle-group';
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import LogInIcon from '@lucide/svelte/icons/log-in';
	import LogOutIcon from '@lucide/svelte/icons/log-out';
	import SunIcon from '@lucide/svelte/icons/sun';
	import MoonIcon from '@lucide/svelte/icons/moon';
	import MonitorIcon from '@lucide/svelte/icons/monitor';
	import { userPrefersMode, setMode } from 'mode-watcher';
	import { userStore, signIn, signOut, displayName, initials } from '$lib/user.svelte';

	const user = $derived(userStore.user);
	const org = $derived(userStore.activeOrg);
</script>

<div class="w-full">
	{#if user === undefined}
		<!-- Skeleton mirroring the profile + session cards. -->
		<div class="flex flex-col gap-6">
			<div class="flex flex-col gap-2">
				<Skeleton class="h-7 w-32" />
				<Skeleton class="h-4 w-56" />
			</div>
			<Card.Root>
				<Card.Header class="flex-row items-center gap-4">
					<Skeleton class="size-14 rounded-full" />
					<div class="flex flex-1 flex-col gap-2">
						<Skeleton class="h-5 w-40" />
						<Skeleton class="h-4 w-52" />
					</div>
				</Card.Header>
				<Card.Content>
					<div class="flex flex-col gap-3">
						{#each [0, 1, 2, 3, 4] as i (i)}
							<div class="grid grid-cols-1 gap-x-4 gap-y-1 sm:grid-cols-[8rem_1fr]">
								<Skeleton class="h-4 w-24" />
								<Skeleton class="h-4 w-40" />
							</div>
						{/each}
					</div>
				</Card.Content>
			</Card.Root>
			<Card.Root>
				<Card.Header>
					<Skeleton class="h-4 w-20" />
					<Skeleton class="h-4 w-64" />
				</Card.Header>
				<Card.Content>
					<Skeleton class="h-9 w-24" />
				</Card.Content>
			</Card.Root>
		</div>
	{:else if user === null}
		<Card.Root>
			<Card.Header>
				<Card.Title>Profile</Card.Title>
				<Card.Description>You're signed out.</Card.Description>
			</Card.Header>
			<Card.Content>
				<Button onclick={signIn}>
					<LogInIcon data-icon="inline-start" />
					Sign in
				</Button>
			</Card.Content>
		</Card.Root>
	{:else}
		<div class="flex flex-col gap-6">
			<div>
				<h1 class="text-2xl font-semibold tracking-tight">Profile</h1>
				<p class="text-muted-foreground text-sm">Your account details and session.</p>
			</div>

			<Card.Root>
				<Card.Header class="flex-row items-center gap-4">
					<Avatar.Root class="size-14">
						<Avatar.Fallback class="bg-primary text-primary-foreground text-lg font-medium">
							{initials(user)}
						</Avatar.Fallback>
					</Avatar.Root>
					<div class="flex min-w-0 flex-col">
						<Card.Title class="truncate text-lg">{displayName(user)}</Card.Title>
						<Card.Description class="truncate">{user.email}</Card.Description>
					</div>
				</Card.Header>
				<Card.Content>
					<dl class="grid grid-cols-1 gap-x-4 gap-y-3 text-sm sm:grid-cols-[8rem_1fr]">
						<dt class="text-muted-foreground">First name</dt>
						<dd>{user.firstName || '—'}</dd>

						<dt class="text-muted-foreground">Last name</dt>
						<dd>{user.lastName || '—'}</dd>

						<dt class="text-muted-foreground">Email</dt>
						<dd class="truncate">{user.email}</dd>

						<dt class="text-muted-foreground">Organization</dt>
						<dd>{org?.name ?? '—'}</dd>

						<dt class="text-muted-foreground">Role</dt>
						<dd>{user.role ?? '—'}</dd>
					</dl>
				</Card.Content>
			</Card.Root>

			<Card.Root>
				<Card.Header>
					<Card.Title class="text-base">Appearance</Card.Title>
					<Card.Description>Choose how the interface looks on this device.</Card.Description>
				</Card.Header>
				<Card.Content>
					<ToggleGroup.Root
						type="single"
						variant="outline"
						spacing={2}
						value={userPrefersMode.current}
						onValueChange={(v) => v && setMode(v as 'light' | 'dark' | 'system')}
						aria-label="Theme"
					>
						<ToggleGroup.Item value="light" class="gap-2">
							<SunIcon class="size-4" /> Light
						</ToggleGroup.Item>
						<ToggleGroup.Item value="dark" class="gap-2">
							<MoonIcon class="size-4" /> Dark
						</ToggleGroup.Item>
						<ToggleGroup.Item value="system" class="gap-2">
							<MonitorIcon class="size-4" /> System
						</ToggleGroup.Item>
					</ToggleGroup.Root>
				</Card.Content>
			</Card.Root>

			<Card.Root>
				<Card.Header>
					<Card.Title class="text-base">Session</Card.Title>
					<Card.Description>Sign out of your account on this device.</Card.Description>
				</Card.Header>
				<Card.Content>
					<Button variant="destructive" onclick={signOut}>
						<LogOutIcon data-icon="inline-start" />
						Log out
					</Button>
				</Card.Content>
			</Card.Root>
		</div>
	{/if}
</div>
