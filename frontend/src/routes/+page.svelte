<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import Logo from '$shared/components/logo.svelte';
	import GithubIcon from '$lib/icons/github.svelte';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import BuildingIcon from '@lucide/svelte/icons/building-2';
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

{#if user && !needsVerify && !needsSelectOrg}
	<!-- Signed in: the $effect above redirects to /dashboard/linear; show a brief
	     placeholder while navigation happens so the page isn't blank. -->
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
					{#if needsVerify}
						Check your email
					{:else if needsSelectOrg}
						Choose an organization
					{:else}
						Streamline your notifications
					{/if}
				</h1>
				<p class="text-muted-foreground max-w-xs text-sm leading-relaxed text-balance">
					{#if needsVerify}
						Enter the verification code we emailed you to finish signing in.
					{:else if needsSelectOrg}
						Pick the organization you want to sign in to.
					{:else}
						Route your Linear and GitHub notifications into one calm Slack feed.
					{/if}
				</p>
			</div>

			<div class="mt-6 flex flex-col gap-4">
				{#if needsVerify}
					<!-- Email verification step (e.g. first GitHub OAuth login). -->
					<form class="flex flex-col gap-3" onsubmit={submitVerifyCode}>
						<input
							class="border-input bg-background/60 focus-visible:ring-ring rounded-md border px-3 py-2 text-center font-mono text-base tracking-[0.4em] focus-visible:ring-2 focus-visible:outline-none"
							type="text"
							inputmode="numeric"
							autocomplete="one-time-code"
							placeholder="000000"
							bind:value={verifyCode}
							disabled={verifying}
							required
						/>
						<Button type="submit" size="lg" disabled={verifying || verifyCode.trim() === ''}>
							{#if verifying}
								<LoaderIcon data-icon="inline-start" class="animate-spin" />
								Verifying…
							{:else}
								Verify and sign in
							{/if}
						</Button>
					</form>
					{#if verifyError}
						<p class="text-destructive text-center text-sm">{verifyError}</p>
					{/if}
				{:else if needsSelectOrg}
					<!-- Organization selection step (user belongs to multiple orgs). -->
					{#if pendingOrgs.length === 0}
						<p class="text-muted-foreground flex items-center justify-center gap-2 text-sm">
							<LoaderIcon class="size-4 animate-spin" />
							Loading organizations…
						</p>
					{:else}
						<div class="flex flex-col gap-2">
							{#each pendingOrgs as org (org.id)}
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
					{/if}
					{#if selectOrgError}
						<p class="text-destructive text-center text-sm">{selectOrgError}</p>
					{/if}
				{:else if user === undefined}
					<!-- Still checking the session: skeleton in place of the action. -->
					<Skeleton class="h-11 w-full rounded-md" />
				{:else}
					<!-- Signed out (or store cleared after a failed request). -->
					<Button onclick={signIn} size="lg" class="font-medium">
						<GithubIcon data-icon="inline-start" size={18} />
						Continue with GitHub
					</Button>
					<p class="text-muted-foreground/80 text-center text-xs">
						We only read your identity to sign you in.
					</p>
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
