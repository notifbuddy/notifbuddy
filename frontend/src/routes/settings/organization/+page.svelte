<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import * as Avatar from '$lib/components/ui/avatar';
	import { Badge } from '$lib/components/ui/badge';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import UserPlusIcon from '@lucide/svelte/icons/user-plus';
	import MailIcon from '@lucide/svelte/icons/mail';
	import { userStore } from '$lib/user.svelte';
	import {
		fetchMembers,
		fetchInvitations,
		sendInvitation,
		memberName,
		memberInitials,
		type Member,
		type Invitation
	} from '$lib/organization';

	const user = $derived(userStore.user);
	const org = $derived(userStore.activeOrg);

	let members = $state<Member[] | null | undefined>(undefined);
	let invitations = $state<Invitation[] | null | undefined>(undefined);

	let inviteEmail = $state('');
	let inviteRole = $state('');
	let inviting = $state(false);
	let inviteMsg = $state<string | null>(null);

	async function loadMembers() {
		members = await fetchMembers();
	}
	async function loadInvitations() {
		invitations = await fetchInvitations();
	}
	loadMembers();
	loadInvitations();

	async function invite(e: SubmitEvent) {
		e.preventDefault();
		inviting = true;
		inviteMsg = null;
		const inv = await sendInvitation(inviteEmail.trim(), inviteRole.trim() || undefined);
		inviting = false;
		if (!inv) {
			inviteMsg = 'Could not send the invitation.';
			return;
		}
		inviteMsg = `Invited ${inv.email}.`;
		inviteEmail = '';
		inviteRole = '';
		loadInvitations();
	}

	// Badge variant per invitation state.
	const inviteBadge = (state: string): 'secondary' | 'outline' | 'destructive' =>
		state === 'accepted' ? 'secondary' : state === 'revoked' || state === 'expired' ? 'destructive' : 'outline';
</script>

<div class="flex flex-col gap-6">
	<div class="flex flex-col gap-1">
		<h1 class="text-2xl font-semibold tracking-tight">Organization</h1>
		<p class="text-muted-foreground text-sm">
			{#if org}Members and invitations for {org.name}.{:else}Members and invitations.{/if}
		</p>
	</div>

	<!-- Members -->
	<section class="flex flex-col gap-3">
		<h2 class="text-sm font-medium">Members</h2>
		{#if members === undefined}
			<Card.Root class="gap-0 divide-y py-0">
				{#each [0, 1, 2] as i (i)}
					<div class="flex items-center gap-3 p-4">
						<Skeleton class="size-9 shrink-0 rounded-full" />
						<div class="flex flex-1 flex-col gap-2">
							<Skeleton class="h-4 w-40" />
							<Skeleton class="h-3.5 w-56 max-w-full" />
						</div>
						<Skeleton class="h-5 w-16 shrink-0" />
					</div>
				{/each}
			</Card.Root>
		{:else if members === null}
			<p class="text-destructive text-sm">Couldn't load members. Please sign in again.</p>
		{:else if members.length === 0}
			<p class="text-muted-foreground text-sm">No members yet.</p>
		{:else}
			<Card.Root class="gap-0 divide-y py-0">
				{#each members as m (m.id)}
					<div class="flex items-center gap-3 p-4">
						<Avatar.Root class="size-9">
							<Avatar.Fallback class="bg-primary text-primary-foreground text-xs font-medium">
								{memberInitials(m)}
							</Avatar.Fallback>
						</Avatar.Root>
						<div class="flex min-w-0 flex-1 flex-col">
							<span class="flex items-center gap-2 truncate font-medium">
								{memberName(m)}
								{#if m.userId === user?.id}<span class="text-muted-foreground text-xs font-normal">(you)</span>{/if}
							</span>
							<span class="text-muted-foreground truncate text-sm">{m.email}</span>
						</div>
						{#if m.role}
							<Badge variant="secondary" class="shrink-0">{m.role}</Badge>
						{/if}
					</div>
				{/each}
			</Card.Root>
		{/if}
	</section>

	<!-- Invitations -->
	<section class="flex flex-col gap-3">
		<h2 class="text-sm font-medium">Invitations</h2>

		<!-- Invite form -->
		<Card.Root>
			<Card.Content>
				<form class="flex flex-col gap-2 sm:flex-row" onsubmit={invite}>
					<input
						class="border-input bg-background focus-visible:ring-ring flex-1 rounded-md border px-3 py-2 text-sm focus-visible:ring-2 focus-visible:outline-none"
						type="email"
						placeholder="teammate@example.com"
						bind:value={inviteEmail}
						disabled={inviting}
						required
					/>
					<input
						class="border-input bg-background focus-visible:ring-ring rounded-md border px-3 py-2 text-sm focus-visible:ring-2 focus-visible:outline-none sm:w-40"
						type="text"
						placeholder="role (optional)"
						bind:value={inviteRole}
						disabled={inviting}
					/>
					<Button type="submit" disabled={inviting || inviteEmail.trim() === ''}>
						{#if inviting}
							<LoaderIcon data-icon="inline-start" class="animate-spin" />
							Sending…
						{:else}
							<UserPlusIcon data-icon="inline-start" />
							Invite
						{/if}
					</Button>
				</form>
				{#if inviteMsg}<p class="text-muted-foreground mt-2 text-sm">{inviteMsg}</p>{/if}
			</Card.Content>
		</Card.Root>

		<!-- Invitations list -->
		{#if invitations === undefined}
			<Card.Root class="gap-0 divide-y py-0">
				{#each [0, 1] as i (i)}
					<div class="flex items-center justify-between gap-3 p-4">
						<div class="flex items-center gap-3">
							<Skeleton class="size-9 shrink-0 rounded-full" />
							<Skeleton class="h-4 w-48" />
						</div>
						<Skeleton class="h-5 w-16 shrink-0" />
					</div>
				{/each}
			</Card.Root>
		{:else if invitations === null}
			<p class="text-destructive text-sm">Couldn't load invitations.</p>
		{:else if invitations.length === 0}
			<p class="text-muted-foreground text-sm">No invitations sent yet.</p>
		{:else}
			<Card.Root class="gap-0 divide-y py-0">
				{#each invitations as inv (inv.id)}
					<div class="flex items-center justify-between gap-3 p-4">
						<div class="flex min-w-0 items-center gap-3">
							<div class="bg-muted text-muted-foreground flex size-9 shrink-0 items-center justify-center rounded-full">
								<MailIcon class="size-4" />
							</div>
							<span class="truncate text-sm">{inv.email}</span>
						</div>
						<Badge variant={inviteBadge(inv.state)} class="shrink-0 capitalize">{inv.state}</Badge>
					</div>
				{/each}
			</Card.Root>
		{/if}
	</section>
</div>
