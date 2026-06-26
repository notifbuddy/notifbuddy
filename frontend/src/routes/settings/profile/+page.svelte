<script lang="ts">
	import * as Card from '$lib/components/ui/card';
	import * as Avatar from '$lib/components/ui/avatar';
	import { Button } from '$lib/components/ui/button';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import LogInIcon from '@lucide/svelte/icons/log-in';
	import LogOutIcon from '@lucide/svelte/icons/log-out';
	import { userStore, signIn, signOut, displayName, initials } from '$lib/user.svelte';

	const user = $derived(userStore.user);
	const org = $derived(userStore.activeOrg);
</script>

<div class="w-full">
	{#if user === undefined}
		<p class="text-muted-foreground flex items-center gap-2 text-sm">
			<LoaderIcon class="animate-spin" /> Loading your profile…
		</p>
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
