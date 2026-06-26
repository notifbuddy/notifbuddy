<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import CheckIcon from '@lucide/svelte/icons/circle-check-big';
	import GithubIcon from '@lucide/svelte/icons/git-branch';
	import SlackIcon from '@lucide/svelte/icons/message-square';
	import {
		fetchIntegrationStatus,
		connect,
		statusOf,
		type IntegrationState
	} from '$lib/integrations';

	let intg = $state<IntegrationState | null | undefined>(undefined); // undefined = loading
	let notice = $state<string | null>(null);

	// On return from a connect redirect the backend appends ?provider=&status=.
	const params = typeof window !== 'undefined' ? new URLSearchParams(window.location.search) : null;
	const returnedProvider = params?.get('provider');
	const returnedStatus = params?.get('status');

	async function load() {
		intg = await fetchIntegrationStatus();
		if (returnedProvider && returnedStatus === 'connected') {
			notice = `${returnedProvider === 'github' ? 'GitHub' : 'Slack'} connected.`;
		} else if (returnedStatus === 'pending') {
			notice = 'GitHub installation needs an org admin to approve it. Once approved, reconnect.';
		} else if (returnedStatus === 'error') {
			notice = 'That connection did not complete. Please try again.';
		}
		// Clean the query so a refresh doesn't repeat the notice.
		if (params?.size) history.replaceState(null, '', window.location.pathname);
	}
	load();

	const github = $derived(statusOf(intg ?? null, 'github'));
	const slack = $derived(statusOf(intg ?? null, 'slack'));
	const githubDone = $derived(!!github?.connected);
	const slackDone = $derived(!!slack?.connected);
	const allDone = $derived(githubDone && slackDone);
</script>

<main class="flex min-h-svh items-center justify-center p-6">
	<Card.Root class="w-full max-w-md">
		<Card.Header>
			<Card.Title>Set up your integrations</Card.Title>
			<Card.Description>Connect GitHub, then Slack, to finish onboarding.</Card.Description>
		</Card.Header>
		<Card.Content class="flex flex-col gap-4">
			{#if intg === undefined}
				<p class="text-muted-foreground flex items-center gap-2 text-sm">
					<LoaderIcon class="animate-spin" /> Loading…
				</p>
			{:else if intg === null}
				<p class="text-destructive text-sm">Please sign in first.</p>
				<Button href="/">Go to sign in</Button>
			{:else if intg.configured === false}
				<p class="text-muted-foreground text-sm">
					Integrations aren't configured on the server yet.
				</p>
			{:else}
				{#if notice}
					<p class="bg-muted rounded-md px-3 py-2 text-sm">{notice}</p>
				{/if}

				<!-- Step 1: GitHub -->
				<div class="flex items-start gap-3 rounded-lg border p-3">
					<GithubIcon class="mt-0.5 size-5 shrink-0" />
					<div class="flex flex-1 flex-col gap-2">
						<div class="flex items-center justify-between">
							<span class="font-medium">1. GitHub</span>
							{#if githubDone}
								<span class="text-primary flex items-center gap-1 text-sm">
									<CheckIcon class="size-4" /> Connected
								</span>
							{/if}
						</div>
						{#if githubDone}
							<p class="text-muted-foreground text-sm">
								{github?.account ? `Installed on ${github.account}.` : 'Installed.'}
							</p>
						{:else}
							<Button onclick={() => connect('github')}>Connect GitHub</Button>
						{/if}
					</div>
				</div>

				<!-- Step 2: Slack (gated on GitHub) -->
				<div
					class="flex items-start gap-3 rounded-lg border p-3"
					class:opacity-50={!githubDone}
				>
					<SlackIcon class="mt-0.5 size-5 shrink-0" />
					<div class="flex flex-1 flex-col gap-2">
						<div class="flex items-center justify-between">
							<span class="font-medium">2. Slack</span>
							{#if slackDone}
								<span class="text-primary flex items-center gap-1 text-sm">
									<CheckIcon class="size-4" /> Connected
								</span>
							{/if}
						</div>
						{#if slackDone}
							<p class="text-muted-foreground text-sm">
								{slack?.account ? `Connected to ${slack.account}.` : 'Connected.'}
							</p>
						{:else}
							<Button onclick={() => connect('slack')} disabled={!githubDone}>
								Connect Slack
							</Button>
							{#if !githubDone}
								<p class="text-muted-foreground text-xs">Connect GitHub first.</p>
							{/if}
						{/if}
					</div>
				</div>

				{#if allDone}
					<div class="flex flex-col gap-2 border-t pt-3">
						<p class="text-primary flex items-center gap-2 text-sm font-medium">
							<CheckIcon class="size-4" /> All set — both integrations are connected.
						</p>
						<Button href="/">Go to dashboard</Button>
					</div>
				{:else}
					<Button variant="ghost" href="/settings/integrations">Manage integrations instead</Button>
				{/if}
			{/if}
		</Card.Content>
	</Card.Root>
</main>
