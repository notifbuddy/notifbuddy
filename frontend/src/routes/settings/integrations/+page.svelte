<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import GithubIcon from '@lucide/svelte/icons/git-branch';
	import SlackIcon from '@lucide/svelte/icons/message-square';
	import {
		fetchIntegrationStatus,
		connect,
		disconnect,
		statusOf,
		PROVIDERS,
		type IntegrationState
	} from '$lib/integrations';

	let intg = $state<IntegrationState | null | undefined>(undefined);
	let busy = $state<string | null>(null);

	const params = typeof window !== 'undefined' ? new URLSearchParams(window.location.search) : null;

	async function load() {
		intg = await fetchIntegrationStatus();
		if (params?.size) history.replaceState(null, '', window.location.pathname);
	}
	load();

	async function doDisconnect(provider: string) {
		busy = provider;
		const next = await disconnect(provider);
		busy = null;
		if (next) intg = next;
	}

	const icon = (key: string) => (key === 'github' ? GithubIcon : SlackIcon);
</script>

<main class="flex min-h-svh items-center justify-center p-6">
	<Card.Root class="w-full max-w-md">
		<Card.Header>
			<Card.Title>Integrations</Card.Title>
			<Card.Description>Manage the connections for your organization.</Card.Description>
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
				{#each PROVIDERS as p (p.key)}
					{@const s = statusOf(intg, p.key)}
					{@const Icon = icon(p.key)}
					<div class="flex items-start gap-3 rounded-lg border p-3">
						<Icon class="mt-0.5 size-5 shrink-0" />
						<div class="flex flex-1 flex-col gap-2">
							<div class="flex items-center justify-between">
								<span class="font-medium">{p.label}</span>
								{#if s?.connected}
									<span class="text-primary text-sm">Connected</span>
								{:else}
									<span class="text-muted-foreground text-sm">Not connected</span>
								{/if}
							</div>
							<p class="text-muted-foreground text-sm">
								{#if s?.connected}
									{s.account ? `Connected to ${s.account}.` : 'Connected.'}
								{:else}
									{p.blurb}
								{/if}
							</p>
							<div class="flex gap-2">
								{#if s?.connected}
									<Button variant="outline" size="sm" onclick={() => connect(p.key)}>
										Reconnect
									</Button>
									<Button
										variant="destructive"
										size="sm"
										disabled={busy === p.key}
										onclick={() => doDisconnect(p.key)}
									>
										{#if busy === p.key}
											<LoaderIcon data-icon="inline-start" class="animate-spin" />
										{/if}
										Disconnect
									</Button>
								{:else}
									<Button size="sm" onclick={() => connect(p.key)}>Connect</Button>
								{/if}
							</div>
						</div>
					</div>
				{/each}

				<Button variant="ghost" href="/">Back to dashboard</Button>
			{/if}
		</Card.Content>
	</Card.Root>
</main>
