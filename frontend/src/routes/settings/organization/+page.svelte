<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import * as Avatar from '$lib/components/ui/avatar';
	import { Badge } from '$lib/components/ui/badge';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import * as Select from '$lib/components/ui/select';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import UserPlusIcon from '@lucide/svelte/icons/user-plus';
	import MailIcon from '@lucide/svelte/icons/mail';
	import XIcon from '@lucide/svelte/icons/x';
	import { userStore } from '$lib/user.svelte';
	import {
		fetchMembers,
		fetchInvitations,
		sendInvitation,
		updateMemberRole,
		revokeInvitation,
		memberName,
		memberInitials,
		ROLES,
		type Member,
		type Invitation,
		type Role
	} from '$lib/organization';

	const user = $derived(userStore.user);
	const org = $derived(userStore.activeOrg);
	const isAdmin = $derived(user?.role === 'admin');

	let members = $state<Member[] | null | undefined>(undefined);
	let invitations = $state<Invitation[] | null | undefined>(undefined);

	// Revoked invitations are dead ends — keep them out of the list. Revoking
	// one live therefore removes its row.
	const visibleInvitations = $derived(invitations?.filter((i) => i.state !== 'revoked'));

	let inviteEmail = $state('');
	let inviteRole = $state<Role>('member');
	let inviting = $state(false);
	let inviteMsg = $state<string | null>(null);

	// The membership id whose role change is in flight, and the last failure.
	let roleBusy = $state<string | null>(null);
	let roleError = $state<string | null>(null);

	// Sentence-case label for a role slug.
	const roleLabel = (r: string) => r.charAt(0).toUpperCase() + r.slice(1);

	async function changeRole(m: Member, role: Role) {
		if (role === m.role || roleBusy) return;
		roleBusy = m.id;
		roleError = null;
		const updated = await updateMemberRole(m.id, role);
		roleBusy = null;
		if (!updated) {
			roleError = `Couldn't change ${memberName(m)}'s role.`;
			return;
		}
		members = members?.map((x) => (x.id === m.id ? updated : x));
	}

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
		const inv = await sendInvitation(inviteEmail.trim(), inviteRole);
		inviting = false;
		if (!inv) {
			inviteMsg = 'Could not send the invitation.';
			return;
		}
		inviteMsg = `Invited ${inv.email}.`;
		inviteEmail = '';
		inviteRole = 'member';
		loadInvitations();
	}

	// Badge variant per invitation state.
	const inviteBadge = (state: string): 'secondary' | 'outline' | 'destructive' =>
		state === 'accepted' ? 'secondary' : state === 'revoked' || state === 'expired' ? 'destructive' : 'outline';

	// The invitation id whose revocation is in flight, and the last failure.
	let revoking = $state<string | null>(null);
	let revokeError = $state<string | null>(null);

	async function revoke(inv: Invitation) {
		if (revoking) return;
		revoking = inv.id;
		revokeError = null;
		const updated = await revokeInvitation(inv.id);
		revoking = null;
		if (!updated) {
			revokeError = `Couldn't revoke the invitation for ${inv.email}.`;
			return;
		}
		invitations = invitations?.map((x) => (x.id === inv.id ? updated : x));
	}
</script>

<div class="flex flex-col gap-6">
	<div class="flex flex-col gap-1">
		<h1 class="text-2xl font-semibold tracking-tight">Organization</h1>
		<p class="text-muted-foreground text-sm">
			{#if org}Members and invitations for {org.name}.{:else}Members and invitations.{/if}
		</p>
	</div>

	<!-- Members -->
	<Card.Root>
		<Card.Header>
			<Card.Title class="text-base">Members</Card.Title>
		</Card.Header>
		<Card.Content>
			{#if members === undefined}
				<div class="flex flex-col divide-y">
					{#each [0, 1, 2] as i (i)}
						<div class="flex items-center gap-3 py-3 first:pt-0 last:pb-0">
							<Skeleton class="size-9 shrink-0 rounded-full" />
							<div class="flex flex-1 flex-col gap-2">
								<Skeleton class="h-4 w-40" />
								<Skeleton class="h-3.5 w-56 max-w-full" />
							</div>
							<Skeleton class="h-5 w-16 shrink-0" />
						</div>
					{/each}
				</div>
			{:else if members === null}
				<p class="text-destructive text-sm">Couldn't load members. Please sign in again.</p>
			{:else if members.length === 0}
				<p class="text-muted-foreground text-sm">No members yet.</p>
			{:else}
				<div class="flex flex-col divide-y">
					{#each members as m (m.id)}
						<div class="flex items-center gap-3 py-3 first:pt-0 last:pb-0">
							<Avatar.Root class="size-9">
								{#if m.profilePictureUrl}
									<Avatar.Image src={m.profilePictureUrl} alt={memberName(m)} />
								{/if}
								<Avatar.Fallback class="bg-muted text-muted-foreground text-xs font-medium">
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
							{#if isAdmin && m.userId !== user?.id}
								<Select.Root
									type="single"
									value={m.role ?? ''}
									onValueChange={(v) => changeRole(m, v as Role)}
									disabled={roleBusy === m.id}
								>
									<Select.Trigger class="shrink-0" aria-label="Role for {memberName(m)}">
										{#if roleBusy === m.id}
											<LoaderIcon class="animate-spin" />
										{/if}
										{m.role ? roleLabel(m.role) : 'No role'}
									</Select.Trigger>
									<Select.Content align="end">
										<Select.Group>
											{#each ROLES as role (role)}
												<Select.Item value={role} label={roleLabel(role)}>{roleLabel(role)}</Select.Item>
											{/each}
										</Select.Group>
									</Select.Content>
								</Select.Root>
							{:else if m.role}
								<Badge variant="secondary" class="shrink-0 capitalize">{m.role}</Badge>
							{/if}
						</div>
					{/each}
				</div>
				{#if roleError}<p class="text-destructive mt-2 text-sm">{roleError}</p>{/if}
			{/if}
		</Card.Content>
	</Card.Root>

	<!-- Invitations -->
	<Card.Root>
		<Card.Header>
			<Card.Title class="text-base">Invitations</Card.Title>
			<Card.Description>Invite teammates to {org?.name ?? 'your organization'}.</Card.Description>
		</Card.Header>
		<Card.Content class="flex flex-col gap-4">
			<!-- Invite form -->
			<form class="flex flex-col gap-2 sm:flex-row" onsubmit={invite}>
				<Input
					class="flex-1"
					type="email"
					placeholder="teammate@example.com"
					bind:value={inviteEmail}
					disabled={inviting}
					required
				/>
				<Select.Root
					type="single"
					value={inviteRole}
					onValueChange={(v) => (inviteRole = v as Role)}
					disabled={inviting}
				>
					<Select.Trigger class="h-9 sm:w-32" aria-label="Role for the invitee">
						{roleLabel(inviteRole)}
					</Select.Trigger>
					<Select.Content>
						<Select.Group>
							{#each ROLES as role (role)}
								<Select.Item value={role} label={roleLabel(role)}>{roleLabel(role)}</Select.Item>
							{/each}
						</Select.Group>
					</Select.Content>
				</Select.Root>
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
			{#if inviteMsg}<p class="text-muted-foreground text-sm">{inviteMsg}</p>{/if}

			<!-- Invitations list -->
		{#if invitations === undefined}
			<div class="flex flex-col divide-y border-t">
				{#each [0, 1] as i (i)}
					<div class="flex items-center justify-between gap-3 py-3">
						<div class="flex items-center gap-3">
							<Skeleton class="size-9 shrink-0 rounded-full" />
							<Skeleton class="h-4 w-48" />
						</div>
						<Skeleton class="h-5 w-16 shrink-0" />
					</div>
				{/each}
			</div>
		{:else if invitations === null}
			<p class="text-destructive text-sm">Couldn't load invitations.</p>
		{:else if !visibleInvitations || visibleInvitations.length === 0}
			<p class="text-muted-foreground text-sm">No invitations sent yet.</p>
		{:else}
			<div class="flex flex-col divide-y border-t">
				{#each visibleInvitations as inv (inv.id)}
					<div class="flex items-center justify-between gap-3 py-3">
						<div class="flex min-w-0 items-center gap-3">
							<div class="bg-muted text-muted-foreground flex size-9 shrink-0 items-center justify-center rounded-full">
								<MailIcon class="size-4" />
							</div>
							<span class="truncate text-sm">{inv.email}</span>
						</div>
						<div class="flex shrink-0 items-center gap-2">
							{#if inv.role}
								<Badge variant="secondary" class="capitalize">{inv.role}</Badge>
							{/if}
							<Badge variant={inviteBadge(inv.state)} class="capitalize">{inv.state}</Badge>
							{#if inv.state === 'pending'}
								<Tooltip.Provider delayDuration={200}>
									<Tooltip.Root>
										<Tooltip.Trigger>
											{#snippet child({ props })}
												<Button
													{...props}
													variant="ghost"
													size="icon-sm"
													onclick={() => revoke(inv)}
													disabled={revoking === inv.id}
													aria-label="Revoke invitation for {inv.email}"
												>
													{#if revoking === inv.id}
														<LoaderIcon class="animate-spin" />
													{:else}
														<XIcon />
													{/if}
												</Button>
											{/snippet}
										</Tooltip.Trigger>
										<Tooltip.Content>Revoke invitation</Tooltip.Content>
									</Tooltip.Root>
								</Tooltip.Provider>
							{/if}
						</div>
					</div>
				{/each}
			</div>
			{#if revokeError}<p class="text-destructive text-sm">{revokeError}</p>{/if}
		{/if}
		</Card.Content>
	</Card.Root>
</div>
