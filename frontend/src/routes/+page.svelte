<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import SendIcon from '@lucide/svelte/icons/send';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import LogInIcon from '@lucide/svelte/icons/log-in';
	import LogOutIcon from '@lucide/svelte/icons/log-out';
	import { api, apiBaseUrl } from '$lib/api/client';

	type User = { id: string; email: string; firstName?: string; lastName?: string };

	// null = unknown (still checking), then either a User or false (signed out).
	let user = $state<User | null | false>(null);

	let pinging = $state(false);
	let result = $state<string | null>(null);
	let error = $state<string | null>(null);

	// Email-verification step. The callback redirects here with ?verify=1 when
	// WorkOS gated login on email verification (e.g. GitHub OAuth). We show a
	// code-entry form instead of the normal signed-out UI.
	const startInVerify =
		typeof window !== 'undefined' && new URLSearchParams(window.location.search).has('verify');
	let needsVerify = $state(startInVerify);
	let verifyCode = $state('');
	let verifying = $state(false);
	let verifyError = $state<string | null>(null);

	// On load, ask the API who we are — unless we're mid email-verification, in
	// which case there's no session yet. The session cookie (if any) rides along
	// via credentials: 'include'. A 401 means signed out.
	async function loadUser() {
		const { data, error: reqError } = await api.GET('/me');
		user = reqError || !data ? false : (data as User);
	}
	if (!startInVerify) loadUser();

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
		// Verified — session cookie is now set. Drop the ?verify flag and show the
		// signed-in UI.
		needsVerify = false;
		user = data as User;
		history.replaceState(null, '', window.location.pathname);
	}

	function signIn() {
		// Full-page navigation to the backend's AuthKit redirect route.
		window.location.href = `${apiBaseUrl}/auth/login`;
	}

	function signOut() {
		window.location.href = `${apiBaseUrl}/auth/logout`;
	}

	async function ping() {
		pinging = true;
		result = null;
		error = null;
		// `data` and `error` are fully typed from the generated OpenAPI schema.
		const { data, error: reqError } = await api.GET('/ping');
		pinging = false;
		if (reqError || !data) {
			// 401 here means the session expired since page load — reflect that.
			error = 'Request failed — you may need to sign in again.';
			user = false;
			return;
		}
		result = data.message;
	}
</script>

<main class="flex min-h-svh items-center justify-center p-6">
	<Card.Root class="w-full max-w-sm">
		<Card.Header>
			<Card.Title>Ping / Pong</Card.Title>
			<Card.Description>
				{#if needsVerify}
					Enter the verification code we emailed you to finish signing in.
				{:else if user}
					Signed in. <code>GET /ping</code> is authenticated with your session.
				{:else}
					Sign in with WorkOS to call the protected <code>GET /ping</code> endpoint.
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
			{:else if user === null}
				<!-- Still checking the session. -->
				<p class="text-muted-foreground flex items-center gap-2 text-sm">
					<LoaderIcon class="animate-spin" />
					Checking session…
				</p>
			{:else if user === false}
				<!-- Signed out. -->
				<Button onclick={signIn}>
					<LogInIcon data-icon="inline-start" />
					Sign in with WorkOS
				</Button>
			{:else}
				<!-- Signed in. -->
				<p class="text-sm">
					Signed in as <span class="font-semibold text-primary">{user.email}</span>
				</p>

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
					<p class="text-sm">
						Server replied: <span class="font-semibold text-primary">{result}</span>
					</p>
				{:else if error}
					<p class="text-destructive text-sm">{error}</p>
				{/if}

				<Button variant="outline" onclick={signOut}>
					<LogOutIcon data-icon="inline-start" />
					Log out
				</Button>
			{/if}
		</Card.Content>
	</Card.Root>
</main>
