<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import Logo from '$shared/components/logo.svelte';
	import GithubIcon from '$lib/icons/github.svelte';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import TerminalIcon from '@lucide/svelte/icons/terminal';
	import BuildingIcon from '@lucide/svelte/icons/building-2';
	import CheckIcon from '@lucide/svelte/icons/check';
	import XIcon from '@lucide/svelte/icons/x';
	import { page } from '$app/state';
	import { api } from '$lib/api/client';
	import { authClient } from '$lib/auth-client';
	import { userStore, switchOrg, type User } from '$lib/user.svelte';

	const user = $derived(userStore.user);

	// The CLI session is minted only when the code is approved, so approval is
	// gated on the browser session having an active organization — that way the
	// CLI never starts life org-less.
	const needsSelectOrg = $derived(
		!!user && !user.organizationId && (user.organizations?.length ?? 0) > 0
	);
	const needsCreateOrg = $derived(
		!!user && !user.organizationId && (user.organizations?.length ?? 0) === 0
	);

	let code = $state(page.url.searchParams.get('user_code') ?? '');
	let step = $state<'enter' | 'confirm' | 'approved' | 'denied'>('enter');
	let busy = $state(false);
	let errorMsg = $state<string | null>(null);

	let selectingOrgId = $state<string | null>(null);
	let orgName = $state('');
	let creatingOrg = $state(false);

	userStore.load();

	async function signInHere() {
		const back = new URL(window.location.href);
		await authClient.signIn.social({ provider: 'github', callbackURL: back.toString() });
	}

	async function chooseOrg(orgId: string) {
		selectingOrgId = orgId;
		await switchOrg(orgId);
	}

	async function submitCreateOrg(e: SubmitEvent) {
		e.preventDefault();
		creatingOrg = true;
		errorMsg = null;
		const { data, error: reqError } = await api.POST('/organizations', {
			body: { name: orgName.trim() }
		});
		creatingOrg = false;
		if (reqError || !data) {
			const msg = (reqError as { message?: string })?.message;
			errorMsg = msg?.trim() ? msg : 'Could not create the organization. Please try again.';
			return;
		}
		userStore.user = data as User;
	}

	function normalized(): string {
		return code.trim().toUpperCase().replace(/\s+/g, '');
	}

	async function verifyCode(e?: SubmitEvent) {
		e?.preventDefault();
		busy = true;
		errorMsg = null;
		const { error } = await authClient.device({ query: { user_code: normalized() } });
		busy = false;
		if (error) {
			errorMsg = 'That code was not recognized. Check your terminal and try again.';
			return;
		}
		step = 'confirm';
	}

	async function approve() {
		busy = true;
		errorMsg = null;
		const { error } = await authClient.device.approve({ userCode: normalized() });
		busy = false;
		if (error) {
			errorMsg = 'Could not approve this code — it may have expired. Start again from the CLI.';
			step = 'enter';
			return;
		}
		step = 'approved';
	}

	async function deny() {
		busy = true;
		errorMsg = null;
		const { error } = await authClient.device.deny({ userCode: normalized() });
		busy = false;
		if (error) {
			errorMsg = 'Could not deny this code — it may have expired already.';
			step = 'enter';
			return;
		}
		step = 'denied';
	}
</script>

<svelte:head>
	<title>Authorize CLI — notifbuddy</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<main class="flex min-h-svh items-center justify-center p-6">
	<section class="bg-card w-full max-w-sm rounded-xl border p-6 shadow-sm">
		<div class="flex flex-col items-center gap-1.5 text-center">
			<Logo size={34} />
			<h1 class="mt-3 text-lg font-semibold tracking-tight">
				{#if step === 'approved'}
					CLI authorized
				{:else if step === 'denied'}
					Request denied
				{:else if needsSelectOrg}
					Choose an organization
				{:else if needsCreateOrg}
					Create your organization
				{:else}
					Authorize the notifbuddy CLI
				{/if}
			</h1>
			<p class="text-muted-foreground max-w-xs text-sm leading-relaxed text-balance">
				{#if step === 'approved'}
					You're signed in on this device. Return to your terminal to continue.
				{:else if step === 'denied'}
					The sign-in request was denied. You can close this tab.
				{:else if needsSelectOrg}
					The CLI works within an organization — pick the one to use.
				{:else if needsCreateOrg}
					The CLI works within an organization — name yours to continue.
				{:else if step === 'confirm'}
					Only approve if you just ran <code class="font-mono">notifbuddy login</code> yourself.
				{:else}
					Enter the code shown in your terminal.
				{/if}
			</p>
		</div>

		<div class="mt-6 flex flex-col gap-4">
			{#if user === undefined}
				<Skeleton class="h-11 w-full rounded-md" />
			{:else if !user}
				<Button onclick={signInHere} size="lg" class="font-medium">
					<GithubIcon data-icon="inline-start" size={18} />
					Continue with GitHub
				</Button>
			{:else if needsSelectOrg}
				<div class="flex flex-col gap-2">
					{#each user?.organizations ?? [] as org (org.id)}
						<Button
							variant="outline"
							size="lg"
							class="justify-start"
							onclick={() => chooseOrg(org.id)}
							disabled={selectingOrgId !== null}
						>
							{#if selectingOrgId === org.id}
								<LoaderIcon data-icon="inline-start" class="animate-spin" />
							{:else}
								<BuildingIcon data-icon="inline-start" class="text-muted-foreground" />
							{/if}
							{org.name}
						</Button>
					{/each}
				</div>
			{:else if needsCreateOrg}
				<form class="flex flex-col gap-3" onsubmit={submitCreateOrg}>
					<input
						class="border-input bg-background/60 focus-visible:ring-ring rounded-md border px-3 py-2 text-base focus-visible:ring-2 focus-visible:outline-none"
						type="text"
						placeholder="Acme Inc"
						maxlength="100"
						bind:value={orgName}
						disabled={creatingOrg}
						required
					/>
					<Button type="submit" size="lg" disabled={creatingOrg || orgName.trim() === ''}>
						{#if creatingOrg}
							<LoaderIcon data-icon="inline-start" class="animate-spin" />
							Creating…
						{:else}
							<BuildingIcon data-icon="inline-start" />
							Create organization
						{/if}
					</Button>
				</form>
			{:else if step === 'enter'}
				<form class="flex flex-col gap-3" onsubmit={verifyCode}>
					<input
						class="border-input bg-background/60 focus-visible:ring-ring rounded-md border px-3 py-2 text-center font-mono text-lg tracking-widest uppercase focus-visible:ring-2 focus-visible:outline-none"
						type="text"
						placeholder="XXXX-XXXX"
						maxlength="16"
						bind:value={code}
						disabled={busy}
						required
					/>
					<Button type="submit" size="lg" disabled={busy || normalized() === ''}>
						{#if busy}
							<LoaderIcon data-icon="inline-start" class="animate-spin" />
							Checking…
						{:else}
							<TerminalIcon data-icon="inline-start" />
							Continue
						{/if}
					</Button>
				</form>
			{:else if step === 'confirm'}
				<div class="bg-muted rounded-md py-3 text-center font-mono text-lg tracking-widest">
					{normalized()}
				</div>
				<div class="flex gap-2">
					<Button variant="outline" size="lg" class="flex-1" onclick={deny} disabled={busy}>
						<XIcon data-icon="inline-start" />
						Deny
					</Button>
					<Button size="lg" class="flex-1" onclick={approve} disabled={busy}>
						{#if busy}
							<LoaderIcon data-icon="inline-start" class="animate-spin" />
						{:else}
							<CheckIcon data-icon="inline-start" />
						{/if}
						Approve
					</Button>
				</div>
			{:else if step === 'approved'}
				<div class="text-primary flex items-center justify-center gap-2 text-sm font-medium">
					<CheckIcon size={18} />
					Signed in — return to your terminal
				</div>
			{:else}
				<div class="text-muted-foreground flex items-center justify-center gap-2 text-sm">
					<XIcon size={18} />
					Denied
				</div>
			{/if}

			{#if errorMsg}
				<p class="text-destructive text-center text-sm">{errorMsg}</p>
			{/if}
		</div>
	</section>
</main>
