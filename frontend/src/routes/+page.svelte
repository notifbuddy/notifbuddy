<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import LogInIcon from '@lucide/svelte/icons/log-in';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api/client';
	import { userStore, signIn, type User, type Organization as Org } from '$lib/user.svelte';

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

	// Boot: pick the right entry path. If a step is in progress there's no session
	// yet, so we don't call /me.
	if (startInSelectOrg) {
		loadPendingOrgs();
	} else if (!startInVerify) {
		loadUser();
	}

	// This is the entry route: it owns the signed-out / mid-login UI. Once the
	// session resolves to a real user (and we're not mid verify/select-org), send
	// them into the app at the first dashboard product.
	$effect(() => {
		if (user && !needsVerify && !needsSelectOrg) goto('/dashboard/linear');
	});

	async function loadUser() {
		await userStore.load();
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
	}
</script>

{#if user && !needsVerify && !needsSelectOrg}
	<!-- Signed in: the $effect above redirects to /dashboard/linear; show a brief
	     placeholder while navigation happens so the page isn't blank. -->
	<main class="flex min-h-svh items-center justify-center p-6">
		<LoaderIcon class="text-muted-foreground animate-spin" />
	</main>
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
