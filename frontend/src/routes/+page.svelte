<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import SendIcon from '@lucide/svelte/icons/send';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import LogInIcon from '@lucide/svelte/icons/log-in';
	import UserPlusIcon from '@lucide/svelte/icons/user-plus';
	import PlugIcon from '@lucide/svelte/icons/plug';
	import { api } from '$lib/api/client';
	import { userStore, signIn, type User, type Organization as Org } from '$lib/user.svelte';
	import {
		fetchIntegrationStatus,
		statusOf,
		type IntegrationState
	} from '$lib/integrations';

	type Invitation = { id: string; email: string; state: string; expiresAt?: string };

	// Shared auth state: undefined = still checking, null = signed out, else User.
	const user = $derived(userStore.user);

	// Which mid-login step (if any) the callback bounced us into.
	const params = typeof window !== 'undefined' ? new URLSearchParams(window.location.search) : null;
	const startInVerify = params?.has('verify') ?? false;
	const startInSelectOrg = params?.has('select-org') ?? false;

	// ---- Email verification step ----
	let needsVerify = $state(startInVerify);
	let verifyCode = $state('');
	let verifying = $state(false);
	let verifyError = $state<string | null>(null);

	// ---- Organization selection step ----
	let needsSelectOrg = $state(startInSelectOrg);
	let pendingOrgs = $state<Org[]>([]);
	let selectingOrgId = $state<string | null>(null);
	let selectOrgError = $state<string | null>(null);

	// ---- Ping ----
	let pinging = $state(false);
	let result = $state<string | null>(null);
	let error = $state<string | null>(null);

	// ---- Invitations ----
	let inviteEmail = $state('');
	let inviteRole = $state('');
	let inviting = $state(false);
	let inviteMsg = $state<string | null>(null);
	let invitations = $state<Invitation[]>([]);

	// ---- Integrations summary ----
	let integrations = $state<IntegrationState | null>(null);

	// Boot: pick the right entry path. If a step is in progress there's no session
	// yet, so we don't call /me.
	if (startInSelectOrg) {
		loadPendingOrgs();
	} else if (!startInVerify) {
		loadUser();
	}

	async function loadUser() {
		const u = await userStore.load();
		if (u) {
			loadInvitations();
			loadIntegrations();
		}
	}

	async function loadIntegrations() {
		integrations = await fetchIntegrationStatus();
	}

	async function loadPendingOrgs() {
		const { data, error: reqError } = await api.GET('/auth/pending-orgs');
		if (reqError || !data) {
			// No pending selection (expired?) — fall back to the normal flow.
			needsSelectOrg = false;
			loadUser();
			return;
		}
		pendingOrgs = (data.organizations ?? []) as Org[];
	}

	async function submitVerifyCode(e: SubmitEvent) {
		e.preventDefault();
		verifying = true;
		verifyError = null;
		const { data, error: reqError } = await api.POST('/auth/verify-email', {
			body: { code: verifyCode.trim() }
		});
		verifying = false;
		if (reqError || !data) {
			verifyError = 'That code was not accepted. Check the code and try again.';
			return;
		}
		finishLogin(data as User);
	}

	async function chooseOrg(orgId: string) {
		selectingOrgId = orgId;
		selectOrgError = null;
		const { data, error: reqError } = await api.POST('/auth/select-org', {
			body: { organizationId: orgId }
		});
		selectingOrgId = null;
		if (reqError || !data) {
			selectOrgError = 'Could not select that organization. Please try signing in again.';
			return;
		}
		finishLogin(data as User);
	}

	// Common tail for verify / select-org: session cookie is now set; show the
	// signed-in UI and clean the URL.
	function finishLogin(u: User) {
		needsVerify = false;
		needsSelectOrg = false;
		userStore.user = u;
		history.replaceState(null, '', window.location.pathname);
		loadInvitations();
		loadIntegrations();
	}

	async function ping() {
		pinging = true;
		result = null;
		error = null;
		const { data, error: reqError } = await api.GET('/ping');
		pinging = false;
		if (reqError || !data) {
			error = 'Request failed — you may need to sign in again.';
			userStore.user = null;
			return;
		}
		result = data.message;
	}

	async function loadInvitations() {
		const { data } = await api.GET('/invitations');
		invitations = data ? ((data.invitations ?? []) as Invitation[]) : [];
	}

	async function sendInvite(e: SubmitEvent) {
		e.preventDefault();
		inviting = true;
		inviteMsg = null;
		const { data, error: reqError } = await api.POST('/invitations', {
			body: { email: inviteEmail.trim(), ...(inviteRole.trim() ? { role: inviteRole.trim() } : {}) }
		});
		inviting = false;
		if (reqError || !data) {
			inviteMsg = 'Could not send the invitation.';
			return;
		}
		inviteMsg = `Invited ${data.email}.`;
		inviteEmail = '';
		inviteRole = '';
		loadInvitations();
	}

	const activeOrg = $derived(userStore.activeOrg);

	const ghConnected = $derived(!!statusOf(integrations, 'github')?.connected);
	const slackConnected = $derived(!!statusOf(integrations, 'slack')?.connected);
	const integrationsComplete = $derived(ghConnected && slackConnected);
</script>

{#if user}
	<!-- Signed in: dashboard content rendered inside the app shell's content area. -->
	<div class="flex flex-col gap-1">
		<h1 class="text-2xl font-semibold tracking-tight">Dashboard</h1>
		<p class="text-muted-foreground text-sm">
			Signed in as <span class="text-foreground font-medium">{user.email}</span>
			{#if activeOrg}
				· {activeOrg.name}{#if user.role}<span class="text-muted-foreground"> · {user.role}</span>{/if}
			{/if}
		</p>
	</div>

	<div class="grid auto-rows-min gap-4 md:grid-cols-2 xl:grid-cols-3">
		<!-- Ping card. -->
		<Card.Root>
			<Card.Header>
				<Card.Title class="text-base">Ping / Pong</Card.Title>
				<Card.Description><code>GET /ping</code> is authenticated with your session.</Card.Description>
			</Card.Header>
			<Card.Content class="flex flex-col gap-3">
				<Button onclick={ping} disabled={pinging}>
					{#if pinging}
						<LoaderIcon data-icon="inline-start" class="animate-spin" />
						Pinging…
					{:else}
						<SendIcon data-icon="inline-start" />
						Send ping
					{/if}
				</Button>
				{#if result}
					<p class="text-sm">Server replied: <span class="text-primary font-semibold">{result}</span></p>
				{:else if error}
					<p class="text-destructive text-sm">{error}</p>
				{/if}
			</Card.Content>
		</Card.Root>

		<!-- Integrations summary card. -->
		{#if integrations && integrations.configured}
			<Card.Root>
				<Card.Header>
					<Card.Title class="flex items-center gap-2 text-base">
						<PlugIcon class="size-4" /> Integrations
					</Card.Title>
					<Card.Description>
						GitHub: <span class="text-foreground">{ghConnected ? 'connected' : 'not connected'}</span>
						· Slack: <span class="text-foreground">{slackConnected ? 'connected' : 'not connected'}</span>
					</Card.Description>
				</Card.Header>
				<Card.Content>
					{#if integrationsComplete}
						<Button variant="outline" size="sm" href="/settings/integrations">Manage integrations</Button>
					{:else}
						<Button size="sm" href="/onboarding">Finish setup</Button>
					{/if}
				</Card.Content>
			</Card.Root>
		{/if}

		<!-- Invite teammate card. -->
		{#if user.organizationId}
			<Card.Root>
				<Card.Header>
					<Card.Title class="text-base">Invite a teammate</Card.Title>
					<Card.Description>Add someone to {activeOrg?.name ?? 'your organization'}.</Card.Description>
				</Card.Header>
				<Card.Content class="flex flex-col gap-2">
					<form class="flex flex-col gap-2" onsubmit={sendInvite}>
						<input
							class="border-input bg-background focus-visible:ring-ring rounded-md border px-3 py-2 text-sm focus-visible:ring-2 focus-visible:outline-none"
							type="email"
							placeholder="teammate@example.com"
							bind:value={inviteEmail}
							disabled={inviting}
							required
						/>
						<input
							class="border-input bg-background focus-visible:ring-ring rounded-md border px-3 py-2 text-sm focus-visible:ring-2 focus-visible:outline-none"
							type="text"
							placeholder="role slug (optional, e.g. member)"
							bind:value={inviteRole}
							disabled={inviting}
						/>
						<Button type="submit" variant="secondary" disabled={inviting || inviteEmail.trim() === ''}>
							{#if inviting}
								<LoaderIcon data-icon="inline-start" class="animate-spin" />
								Sending…
							{:else}
								<UserPlusIcon data-icon="inline-start" />
								Send invite
							{/if}
						</Button>
					</form>
					{#if inviteMsg}<p class="text-muted-foreground text-sm">{inviteMsg}</p>{/if}
					{#if invitations.length > 0}
						<ul class="text-muted-foreground mt-1 flex flex-col gap-1 text-xs">
							{#each invitations as inv (inv.id)}
								<li class="flex justify-between gap-2">
									<span class="truncate">{inv.email}</span>
									<span class="shrink-0">{inv.state}</span>
								</li>
							{/each}
						</ul>
					{/if}
				</Card.Content>
			</Card.Root>
		{/if}
	</div>
{:else}
	<!-- Signed out / mid-login: centered login card (rendered bare, no app shell). -->
	<main class="flex min-h-svh items-center justify-center p-6">
		<Card.Root class="w-full max-w-sm">
			<Card.Header>
				<Card.Title>Sign in</Card.Title>
				<Card.Description>
					{#if needsVerify}
						Enter the verification code we emailed you to finish signing in.
					{:else if needsSelectOrg}
						Choose an organization to sign in to.
					{:else}
						Sign in with WorkOS to access your dashboard.
					{/if}
				</Card.Description>
			</Card.Header>
			<Card.Content class="flex flex-col gap-4">
				{#if needsVerify}
				<!-- Email verification step (e.g. first GitHub OAuth login). -->
				<form class="flex flex-col gap-3" onsubmit={submitVerifyCode}>
					<input
						class="border-input bg-background focus-visible:ring-ring rounded-md border px-3 py-2 text-sm tracking-widest focus-visible:ring-2 focus-visible:outline-none"
						type="text"
						inputmode="numeric"
						autocomplete="one-time-code"
						placeholder="123456"
						bind:value={verifyCode}
						disabled={verifying}
						required
					/>
					<Button type="submit" disabled={verifying || verifyCode.trim() === ''}>
						{#if verifying}
							<LoaderIcon data-icon="inline-start" class="animate-spin" />
							Verifying…
						{:else}
							Verify and sign in
						{/if}
					</Button>
				</form>
				{#if verifyError}
					<p class="text-destructive text-sm">{verifyError}</p>
				{/if}
			{:else if needsSelectOrg}
				<!-- Organization selection step (user belongs to multiple orgs). -->
				{#if pendingOrgs.length === 0}
					<p class="text-muted-foreground flex items-center gap-2 text-sm">
						<LoaderIcon class="animate-spin" />
						Loading organizations…
					</p>
				{:else}
					<div class="flex flex-col gap-2">
						{#each pendingOrgs as org (org.id)}
							<Button
								variant="outline"
								onclick={() => chooseOrg(org.id)}
								disabled={selectingOrgId !== null}
							>
								{#if selectingOrgId === org.id}
									<LoaderIcon data-icon="inline-start" class="animate-spin" />
								{/if}
								{org.name}
							</Button>
						{/each}
					</div>
				{/if}
				{#if selectOrgError}
					<p class="text-destructive text-sm">{selectOrgError}</p>
				{/if}
			{:else if user === undefined}
				<!-- Still checking the session: skeleton in place of the action. -->
				<div class="flex flex-col gap-3">
					<Skeleton class="h-4 w-40" />
					<Skeleton class="h-9 w-full" />
				</div>
			{:else}
				<!-- Signed out (or store cleared after a failed request). -->
				<Button onclick={signIn}>
					<LogInIcon data-icon="inline-start" />
					Sign in with WorkOS
				</Button>
			{/if}
		</Card.Content>
	</Card.Root>
	</main>
{/if}
