<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Dialog from '$lib/components/ui/dialog';
	import { Checkbox } from '$lib/components/ui/checkbox';
	import { Input } from '$lib/components/ui/input';
	import * as Select from '$lib/components/ui/select';
	import * as Avatar from '$lib/components/ui/avatar';
	import { Badge } from '$lib/components/ui/badge';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import { SvelteSet } from 'svelte/reactivity';
	import {
		fetchSlackMembers,
		syncSettings,
		type SlackMember,
		type SlackMemberList
	} from '$lib/integrations';
	import { sendInvitation, ROLES, type Role } from '$lib/organization';

	let {
		open = $bindable(false),
		memberEmails,
		invitedEmails,
		oninvited
	}: {
		open?: boolean;
		memberEmails: string[];
		invitedEmails: string[];
		oninvited: (count: number) => void;
	} = $props();

	let list = $state<SlackMemberList | null | undefined>(undefined);
	let search = $state('');
	let role = $state<Role>('member');
	let sending = $state(false);
	let syncing = $state(false);
	let sentCount = $state(0);
	let sendTotal = $state(0);
	const selected = new SvelteSet<string>();
	const failed = new SvelteSet<string>();
	const invitedNow = new SvelteSet<string>();

	const roleLabel = (r: string) => r.charAt(0).toUpperCase() + r.slice(1);

	$effect(() => {
		if (open && list === undefined) load();
	});

	async function load() {
		list = undefined;
		list = await fetchSlackMembers();
	}

	async function resync() {
		if (syncing) return;
		syncing = true;
		await syncSettings();
		list = await fetchSlackMembers();
		syncing = false;
	}

	const humans = $derived((list?.members ?? []).filter((m) => !m.isBot));
	const hiddenNoEmail = $derived(humans.filter((m) => !m.email).length);
	const listed = $derived(humans.filter((m) => !!m.email));

	const lowerMembers = $derived(new Set(memberEmails.map((e) => e.toLowerCase())));
	const lowerInvited = $derived(new Set(invitedEmails.map((e) => e.toLowerCase())));

	function eligible(m: SlackMember): boolean {
		const email = m.email!.toLowerCase();
		return !lowerMembers.has(email) && !lowerInvited.has(email) && !invitedNow.has(email);
	}

	const candidates = $derived(listed.filter(eligible));
	const alreadyInOrg = $derived(listed.length - candidates.length);

	const visible = $derived(
		candidates.filter((m) => {
			const q = search.trim().toLowerCase();
			if (!q) return true;
			return m.name.toLowerCase().includes(q) || m.email!.toLowerCase().includes(q);
		})
	);
	const allSelected = $derived(
		visible.length > 0 && visible.every((m) => selected.has(m.memberId))
	);

	function toggleAll() {
		if (allSelected) {
			for (const m of visible) selected.delete(m.memberId);
		} else {
			for (const m of visible) selected.add(m.memberId);
		}
	}

	function toggle(m: SlackMember) {
		if (selected.has(m.memberId)) selected.delete(m.memberId);
		else selected.add(m.memberId);
	}

	function timeAgo(iso: string): string {
		const mins = Math.max(0, Math.round((Date.now() - Date.parse(iso)) / 60000));
		if (mins < 1) return 'just now';
		if (mins < 60) return `${mins}m ago`;
		const hours = Math.round(mins / 60);
		if (hours < 24) return `${hours}h ago`;
		return `${Math.round(hours / 24)}d ago`;
	}

	async function sendAll() {
		if (sending) return;
		const targets = candidates.filter((m) => selected.has(m.memberId));
		if (targets.length === 0) return;
		sending = true;
		failed.clear();
		sentCount = 0;
		sendTotal = targets.length;
		let ok = 0;
		for (const m of targets) {
			const inv = await sendInvitation(m.email!, role);
			sentCount += 1;
			if (inv) {
				ok += 1;
				selected.delete(m.memberId);
				invitedNow.add(m.email!.toLowerCase());
			} else {
				failed.add(m.memberId);
			}
		}
		sending = false;
		if (ok > 0) oninvited(ok);
		if (failed.size === 0) open = false;
	}

	function onOpenChange(o: boolean) {
		open = o;
		if (!o) {
			search = '';
			selected.clear();
			failed.clear();
		}
	}
</script>

<Dialog.Root {open} {onOpenChange}>
	<Dialog.Content class="sm:max-w-lg">
		<Dialog.Header>
			<Dialog.Title>Invite from Slack</Dialog.Title>
			<Dialog.Description>
				Pick teammates from your Slack workspace — each gets an email invitation.
			</Dialog.Description>
		</Dialog.Header>

		{#if list === undefined}
			<div class="flex flex-col gap-3">
				{#each [0, 1, 2] as i (i)}
					<div class="flex items-center gap-3">
						<Skeleton class="size-8 shrink-0 rounded-full" />
						<Skeleton class="h-4 flex-1" />
					</div>
				{/each}
			</div>
		{:else if list === null}
			<p class="text-destructive text-sm">Couldn't load Slack members.</p>
		{:else if !list.connected}
			<p class="text-muted-foreground text-sm">
				Slack isn't connected for this organization yet. Connect it in
				<a href="/settings/integrations" class="underline underline-offset-2">Integrations</a>
				to import members.
			</p>
		{:else if candidates.length === 0}
			<div class="flex flex-col gap-3">
				<p class="text-muted-foreground text-sm">
					{#if alreadyInOrg > 0}
						Everyone from Slack with a visible email is already a member or invited.
					{:else}
						No Slack members with a visible email yet. Try syncing to refresh the list.
					{/if}
				</p>
				<Button variant="outline" class="self-start" onclick={resync} disabled={syncing}>
					{#if syncing}
						<LoaderIcon data-icon="inline-start" class="animate-spin" />
					{:else}
						<RefreshCwIcon data-icon="inline-start" />
					{/if}
					Sync from Slack
				</Button>
			</div>
		{:else}
			<div class="flex items-center gap-2">
				<Input
					class="flex-1"
					type="search"
					placeholder="Search by name or email…"
					bind:value={search}
					disabled={sending}
				/>
				{#if list.syncedAt}
					<span class="text-muted-foreground shrink-0 text-xs">Synced {timeAgo(list.syncedAt)}</span>
				{/if}
				<Button
					variant="ghost"
					size="icon-sm"
					class="shrink-0"
					onclick={resync}
					disabled={syncing || sending}
					aria-label="Re-sync Slack members"
				>
					{#if syncing}
						<LoaderIcon class="animate-spin" />
					{:else}
						<RefreshCwIcon />
					{/if}
				</Button>
			</div>

			<button
				type="button"
				class="text-muted-foreground flex items-center gap-3 border-b pb-2 text-sm select-none disabled:opacity-50"
				onclick={toggleAll}
				disabled={sending || visible.length === 0}
			>
				<Checkbox
					checked={allSelected}
					class="pointer-events-none"
					aria-hidden="true"
					tabindex={-1}
				/>
				Select all ({visible.length})
			</button>

			<div class="-mx-1 flex max-h-72 flex-col overflow-y-auto px-1">
				{#each visible as m (m.memberId)}
					<button
						type="button"
						class="hover:bg-muted/50 flex cursor-pointer items-center gap-3 rounded-md px-1 py-2 text-left select-none"
						onclick={() => toggle(m)}
						disabled={sending}
						aria-pressed={selected.has(m.memberId)}
					>
						<Checkbox
							checked={selected.has(m.memberId)}
							class="pointer-events-none"
							aria-hidden="true"
							tabindex={-1}
						/>
						<Avatar.Root class="size-8 shrink-0">
							<Avatar.Image src={m.iconUrl} alt="" />
							<Avatar.Fallback>{m.name.slice(0, 2).toUpperCase()}</Avatar.Fallback>
						</Avatar.Root>
						<div class="flex min-w-0 flex-1 flex-col">
							<span class="truncate text-sm font-medium">{m.name}</span>
							<span class="text-muted-foreground truncate text-xs">{m.email}</span>
						</div>
						{#if failed.has(m.memberId)}
							<Badge variant="destructive" class="shrink-0">Failed</Badge>
						{/if}
					</button>
				{:else}
					<p class="text-muted-foreground py-4 text-center text-sm">No members match "{search}".</p>
				{/each}
			</div>

			{#if alreadyInOrg > 0 || hiddenNoEmail > 0}
				<p class="text-muted-foreground text-xs">
					{[
						alreadyInOrg > 0
							? `${alreadyInOrg} already ${alreadyInOrg === 1 ? 'a member' : 'members'} or invited`
							: '',
						hiddenNoEmail > 0
							? `${hiddenNoEmail} hidden — no email visible from Slack`
							: ''
					]
						.filter(Boolean)
						.join(' · ')}
				</p>
			{/if}
			{#if failed.size > 0 && !sending}
				<p class="text-destructive text-sm">
					{failed.size}
					{failed.size === 1 ? 'invitation' : 'invitations'} failed — they're still selected, try
					again.
				</p>
			{/if}

			<Dialog.Footer class="items-center gap-2 sm:justify-between">
				<Select.Root
					type="single"
					value={role}
					onValueChange={(v) => (role = v as Role)}
					disabled={sending}
				>
					<Select.Trigger class="h-9 w-32" aria-label="Role for the invitees">
						{roleLabel(role)}
					</Select.Trigger>
					<Select.Content>
						<Select.Group>
							{#each ROLES as r (r)}
								<Select.Item value={r} label={roleLabel(r)}>{roleLabel(r)}</Select.Item>
							{/each}
						</Select.Group>
					</Select.Content>
				</Select.Root>
				<div class="flex items-center gap-2">
					<Button variant="outline" onclick={() => onOpenChange(false)} disabled={sending}>
						Cancel
					</Button>
					<Button onclick={sendAll} disabled={sending || selected.size === 0}>
						{#if sending}
							<LoaderIcon data-icon="inline-start" class="animate-spin" />
							Inviting {sentCount}/{sendTotal}…
						{:else}
							Invite {selected.size > 0 ? selected.size : ''}
						{/if}
					</Button>
				</div>
			</Dialog.Footer>
		{/if}
	</Dialog.Content>
</Dialog.Root>
