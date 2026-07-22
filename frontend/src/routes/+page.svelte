<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import Logo from '$shared/components/logo.svelte';
	import GithubIcon from '$lib/icons/github.svelte';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import BuildingIcon from '@lucide/svelte/icons/building-2';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api/client';
	import { userStore, signInWithGithub, signInWithEmail, signUpWithEmail, switchOrg, type User } from '$lib/user.svelte';
	import { featureEmailPassword, featureGithubOauth } from '$lib/runtime-config';

	// Shared auth state: undefined = still checking, null = signed out, else User.
	const user = $derived(userStore.user);

	// ---- Organization steps. A session with orgs but no active one gets the
	// picker; a session with no orgs at all gets the create form. ----
	const needsSelectOrg = $derived(
		!!user && !user.organizationId && (user.organizations?.length ?? 0) > 0
	);
	const needsCreateOrg = $derived(
		!!user && !user.organizationId && (user.organizations?.length ?? 0) === 0
	);
	let selectingOrgId = $state<string | null>(null);
	let orgName = $state('');
	let creatingOrg = $state(false);
	let createOrgError = $state<string | null>(null);

	let emailMode = $state<'signin' | 'signup'>('signin');
	let emailName = $state('');
	let emailAddr = $state('');
	let emailPassword = $state('');
	let emailBusy = $state(false);
	let emailError = $state<string | null>(null);

	userStore.load();

	async function submitEmail(e: SubmitEvent) {
		e.preventDefault();
		emailBusy = true;
		emailError = null;
		try {
			if (emailMode === 'signup') {
				await signUpWithEmail(emailName.trim() || emailAddr.trim(), emailAddr.trim(), emailPassword);
			} else {
				await signInWithEmail(emailAddr.trim(), emailPassword);
			}
		} catch (err) {
			emailError = err instanceof Error ? err.message : 'Authentication failed';
			emailBusy = false;
		}
	}

	// This is the entry route: it owns the signed-out / mid-login UI. Once the
	// session resolves to a user with an active org, send them into the app.
	$effect(() => {
		if (user && !needsSelectOrg && !needsCreateOrg) goto('/dashboard/linear');
	});

	async function chooseOrg(orgId: string) {
		selectingOrgId = orgId;
		await switchOrg(orgId); // sets the active org in authd, then reloads
	}

	async function submitCreateOrg(e: SubmitEvent) {
		e.preventDefault();
		creatingOrg = true;
		createOrgError = null;
		const { data, error: reqError } = await api.POST('/organizations', {
			body: { name: orgName.trim() }
		});
		creatingOrg = false;
		if (reqError || !data) {
			const msg = (reqError as { message?: string })?.message;
			createOrgError = msg?.trim() ? msg : 'Could not create the organization. Please try again.';
			return;
		}
		// The session is now scoped to the new org; the redirect effect fires.
		finishLogin(data as User);
	}

	// Common tail for create-org: the session is now scoped to the new org;
	// the redirect effect fires off the refreshed user.
	function finishLogin(u: User) {
		userStore.user = u;
		history.replaceState(null, '', window.location.pathname);
	}

	// Ambient "noise field" behind the card: the product turns notification noise
	// into one quiet signal, so the background is scattered, dimmed notification
	// pips — the noise — with a single lit clay pip near the panel: the signal.
	// Positions are a fixed, hand-tuned scatter (percent of the viewport) so the
	// field is deterministic (no Math.random, which isn't available here anyway)
	// and reads as intentional rather than confetti. `lit` marks the few pips that
	// glow clay; `d` staggers their drift so nothing pulses in lockstep.
	type Pip = { x: number; y: number; s: number; lit?: boolean; d: number };
	const pips: Pip[] = [
		{ x: 8, y: 18, s: 5, d: 0 },
		{ x: 16, y: 62, s: 4, d: 1.4 },
		{ x: 22, y: 34, s: 6, d: 2.1 },
		{ x: 12, y: 82, s: 3, d: 0.6 },
		{ x: 30, y: 12, s: 4, d: 3.2 },
		{ x: 27, y: 74, s: 5, d: 1.1 },
		{ x: 33, y: 30, s: 7, lit: true, d: 0.9 },
		{ x: 34, y: 90, s: 3, d: 2.6 },
		{ x: 46, y: 22, s: 4, d: 1.8 },
		{ x: 36, y: 68, s: 6, lit: true, d: 0.3 },
		{ x: 54, y: 38, s: 6, d: 2.9 },
		{ x: 58, y: 78, s: 4, d: 1.5 },
		{ x: 62, y: 16, s: 5, d: 0.4 },
		{ x: 67, y: 64, s: 8, lit: true, d: 2.2 },
		{ x: 72, y: 84, s: 3, d: 1.0 },
		{ x: 70, y: 30, s: 4, d: 3.4 },
		{ x: 80, y: 60, s: 5, d: 0.7 },
		{ x: 84, y: 24, s: 6, d: 2.4 },
		{ x: 88, y: 76, s: 4, d: 1.3 },
		{ x: 92, y: 44, s: 5, d: 0.2 }
	];
</script>

{#if user && !needsSelectOrg && !needsCreateOrg}
	<!-- Signed in with an org: the $effect above redirects to /dashboard/linear;
	     show a brief placeholder while navigation happens so the page isn't
	     blank. (Org-less sessions fall through to the create-org card below.) -->
	<main class="flex min-h-svh items-center justify-center p-6">
		<LoaderIcon class="text-muted-foreground animate-spin" />
	</main>
{:else}
	<!-- Signed out / mid-login. Rendered bare (no app shell): this is the whole
	     login scene. The thesis — notifbuddy turns notification noise into one
	     quiet signal — is the environment: a dark field of dimmed notification
	     pips (the noise) resolving toward a single lit clay glow behind a calm,
	     precise panel (the signal). -->
	<main class="login-scene relative flex min-h-svh items-center justify-center overflow-hidden p-6">
		<!-- The noise field + brand halo. Purely decorative. -->
		<div class="noise-field" aria-hidden="true">
			<div class="halo"></div>
			{#each pips as pip, i (i)}
				<span
					class="pip"
					class:lit={pip.lit}
					style="left:{pip.x}%; top:{pip.y}%; --s:{pip.s}px; --d:{pip.d}s;"
				></span>
			{/each}
		</div>

		<!-- The signal: a calm glass panel. -->
		<section class="panel relative w-full max-w-sm">
			<div class="flex flex-col items-center gap-1.5 text-center">
				<Logo size={34} />
				<h1 class="mt-3 text-lg font-semibold tracking-tight">
					{#if needsSelectOrg}
						Choose an organization
					{:else if needsCreateOrg}
						Create your organization
					{:else}
						Streamline your notifications
					{/if}
				</h1>
				<p class="text-muted-foreground max-w-xs text-sm leading-relaxed text-balance">
					{#if needsSelectOrg}
						Pick the organization you want to work in.
					{:else if needsCreateOrg}
						Name the organization your team will share — you can rename it later.
					{:else}
						Route your Linear notifications into one calm Slack feed.
					{/if}
				</p>
			</div>

			<div class="mt-6 flex flex-col gap-4">
				{#if needsSelectOrg}
					<!-- Organization selection: the session has orgs but none active. -->
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
					<!-- Organization creation step (signed in, no organization yet). -->
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
					{#if createOrgError}
						<p class="text-destructive text-center text-sm">{createOrgError}</p>
					{/if}
				{:else if user === undefined}
					<!-- Still checking the session: skeleton in place of the action. -->
					<Skeleton class="h-11 w-full rounded-md" />
				{:else}
					<div class="flex w-full flex-col gap-3">
						{#if featureEmailPassword}
							<form class="flex w-full flex-col gap-3" onsubmit={submitEmail}>
								{#if emailMode === 'signup'}
									<input
										class="border-input bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring flex h-10 w-full rounded-md border px-3 text-sm focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
										type="text"
										name="name"
										placeholder="Name"
										autocomplete="name"
										bind:value={emailName}
										required
									/>
								{/if}
								<input
									class="border-input bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring flex h-10 w-full rounded-md border px-3 text-sm focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
									type="email"
									name="email"
									placeholder="Email"
									autocomplete="email"
									bind:value={emailAddr}
									required
								/>
								<input
									class="border-input bg-background ring-offset-background placeholder:text-muted-foreground focus-visible:ring-ring flex h-10 w-full rounded-md border px-3 text-sm focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none"
									type="password"
									name="password"
									placeholder="Password"
									autocomplete={emailMode === 'signup' ? 'new-password' : 'current-password'}
									bind:value={emailPassword}
									required
									minlength="8"
								/>
								<Button type="submit" size="lg" class="font-medium" disabled={emailBusy}>
									{#if emailBusy}
										<LoaderIcon data-icon="inline-start" class="animate-spin" />
										Please wait…
									{:else if emailMode === 'signup'}
										Create account
									{:else}
										Sign in
									{/if}
								</Button>
								<button
									type="button"
									class="text-muted-foreground hover:text-foreground text-center text-sm underline-offset-4 hover:underline"
									onclick={() => {
										emailMode = emailMode === 'signin' ? 'signup' : 'signin';
										emailError = null;
									}}
								>
									{emailMode === 'signin' ? 'Need an account? Sign up' : 'Have an account? Sign in'}
								</button>
								{#if emailError}
									<p class="text-destructive text-center text-sm">{emailError}</p>
								{/if}
							</form>
						{/if}
						{#if featureGithubOauth}
							<Button onclick={signInWithGithub} size="lg" class="font-medium" variant={featureEmailPassword ? 'outline' : 'default'}>
								<GithubIcon data-icon="inline-start" size={18} />
								Continue with GitHub
							</Button>
						{/if}
					</div>
				{/if}
			</div>
		</section>
	</main>
{/if}

<style>
	/* ---- The login scene ---------------------------------------------------
	   Everything below is scoped to this route's signed-out view. The palette is
	   pulled from the app theme tokens (clay --primary on the warm near-black
	   --background) so the scene stays on-brand in dark mode, the app's default. */

	.login-scene {
		/* A soft vertical lift from the background so the field has depth without
		   introducing a second hue. */
		background:
			radial-gradient(120% 80% at 50% 8%, color-mix(in oklab, var(--primary) 6%, transparent), transparent 60%),
			var(--background);
	}

	/* The noise field spans the viewport; pips are absolutely placed within it. */
	.noise-field {
		position: absolute;
		inset: 0;
		pointer-events: none;
	}

	/* The brand dot at room scale: a diffuse clay glow centered behind the panel.
	   This is the "signal" the whole field resolves toward. */
	.halo {
		position: absolute;
		top: 48%;
		left: 50%;
		height: min(42rem, 82vw);
		width: min(42rem, 82vw);
		translate: -50% -50%;
		border-radius: 9999px;
		background: radial-gradient(
			circle,
			color-mix(in oklab, var(--primary) 26%, transparent) 0%,
			color-mix(in oklab, var(--primary) 9%, transparent) 30%,
			transparent 60%
		);
	}

	/* Notification pips — the noise. Dimmed paper dots scattered across the field,
	   each breathing gently on its own stagger so the field feels alive but calm. */
	.pip {
		position: absolute;
		height: var(--s);
		width: var(--s);
		border-radius: 9999px;
		background: color-mix(in oklab, var(--foreground) 55%, transparent);
		opacity: 0.14;
		translate: -50% -50%;
		animation: pip-breathe 6s ease-in-out infinite;
		animation-delay: var(--d);
	}

	/* The few lit pips glow clay and sit brighter — signal emerging from noise. */
	.pip.lit {
		background: color-mix(in oklab, var(--primary) 88%, white);
		opacity: 0.95;
		box-shadow:
			0 0 8px color-mix(in oklab, var(--primary) 85%, transparent),
			0 0 20px color-mix(in oklab, var(--primary) 55%, transparent),
			0 0 36px color-mix(in oklab, var(--primary) 30%, transparent);
	}

	@keyframes pip-breathe {
		0%,
		100% {
			opacity: 0.1;
		}
		50% {
			opacity: 0.24;
		}
	}
	.pip.lit {
		animation-name: pip-breathe-lit;
	}
	@keyframes pip-breathe-lit {
		0%,
		100% {
			opacity: 0.7;
		}
		50% {
			opacity: 1;
		}
	}

	/* The panel: the calm answer. Frosted glass over the field, a hairline ring,
	   and a soft lift — precise, not decorated. */
	.panel {
		border-radius: var(--radius-xl);
		border: 1px solid color-mix(in oklab, var(--foreground) 12%, transparent);
		background: color-mix(in oklab, var(--card) 90%, transparent);
		padding: 2rem 1.75rem;
		box-shadow:
			0 1px 0 0 color-mix(in oklab, var(--foreground) 8%, transparent) inset,
			0 24px 60px -24px rgb(0 0 0 / 0.8);
		backdrop-filter: blur(18px);
		animation: panel-in 0.5s cubic-bezier(0.22, 1, 0.36, 1) both;
	}

	@keyframes panel-in {
		from {
			opacity: 0;
			transform: translateY(8px) scale(0.99);
		}
		to {
			opacity: 1;
			transform: none;
		}
	}

	/* Respect reduced-motion: hold everything still, keep the composition intact. */
	@media (prefers-reduced-motion: reduce) {
		.pip,
		.panel {
			animation: none;
		}
	}
</style>
