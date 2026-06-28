<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import { Badge } from '$lib/components/ui/badge';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import { SiGithub, SiLinear } from '@icons-pack/svelte-simple-icons';
	import SlackIcon from '$lib/icons/slack.svelte';
	import WebhookIcon from '@lucide/svelte/icons/webhook';
	import PlugIcon from '@lucide/svelte/icons/plug';
	import UnplugIcon from '@lucide/svelte/icons/unplug';
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

	const icon = (key: string) =>
		key === 'github' ? SiGithub : key === 'linear' ? SiLinear : SlackIcon;
</script>

<div class="flex flex-col gap-6">
	<div class="flex flex-col gap-1">
		<h1 class="text-2xl font-semibold tracking-tight">Integrations</h1>
		<p class="text-muted-foreground text-sm">Manage the connections for your organization.</p>
	</div>

	{#if intg === undefined}
		<!-- Skeleton mirroring the connection list. -->
		<Card.Root class="gap-0 divide-y py-0">
			{#each [0, 1] as i (i)}
				<div class="flex flex-wrap items-center gap-4 p-4">
					<Skeleton class="size-5 shrink-0 rounded-md" />
					<div class="flex min-w-0 flex-1 flex-col gap-2">
						<div class="flex items-center gap-2">
							<Skeleton class="h-4 w-20" />
							<Skeleton class="h-5 w-24" />
						</div>
						<Skeleton class="h-3.5 w-64 max-w-full" />
					</div>
					<div class="flex shrink-0 items-center gap-2">
						<Skeleton class="h-8 w-24" />
						<Skeleton class="h-8 w-24" />
					</div>
				</div>
			{/each}
		</Card.Root>
	{:else if intg === null}
		<div class="flex flex-col items-start gap-2">
			<p class="text-destructive text-sm">Please sign in first.</p>
			<Button href="/">Go to sign in</Button>
		</div>
	{:else if intg.configured === false}
		<p class="text-muted-foreground text-sm">Integrations aren't configured on the server yet.</p>
	{:else}
		<Tooltip.Provider delayDuration={200}>
		<Card.Root class="gap-0 divide-y py-0">
			{#each PROVIDERS as p (p.key)}
				{@const s = statusOf(intg, p.key)}
				{@const Icon = icon(p.key)}
				<div class="flex flex-wrap items-center gap-4 p-4">
					<Icon class="size-5 shrink-0" />
					<div class="flex min-w-0 flex-1 flex-col gap-0.5">
						<div class="flex items-center gap-2">
							<span class="font-medium">{p.label}</span>
							{#if s?.connected}
								<Badge variant="secondary">Connected</Badge>
							{:else}
								<Badge variant="outline">Not connected</Badge>
							{/if}
						</div>
						<p class="text-muted-foreground truncate text-sm">
							{#if s?.connected}
								{s.account ? `Connected to ${s.account}.` : 'Connected.'}
							{:else}
								{p.blurb}
							{/if}
						</p>
					</div>
					<div class="flex shrink-0 items-center gap-1">
						{#if s?.connected}
							{@const webhooksHref =
								p.key === 'github'
									? '/settings/integrations/webhooks'
									: p.key === 'linear'
										? '/settings/integrations/linear-webhooks'
										: ''}
							{#if webhooksHref}
								<Tooltip.Root>
									<Tooltip.Trigger>
										{#snippet child({ props })}
											<Button
												{...props}
												variant="ghost"
												size="icon-sm"
												href={webhooksHref}
												aria-label="Webhooks"
											>
												<WebhookIcon />
											</Button>
										{/snippet}
									</Tooltip.Trigger>
									<Tooltip.Content>Webhooks</Tooltip.Content>
								</Tooltip.Root>
							{/if}
							<Tooltip.Root>
								<Tooltip.Trigger>
									{#snippet child({ props })}
										<Button
											{...props}
											variant="destructive"
											size="icon-sm"
											disabled={busy === p.key}
											onclick={() => doDisconnect(p.key)}
											aria-label="Disconnect"
										>
											{#if busy === p.key}
												<LoaderIcon class="animate-spin" />
											{:else}
												<UnplugIcon />
											{/if}
										</Button>
									{/snippet}
								</Tooltip.Trigger>
								<Tooltip.Content>Disconnect</Tooltip.Content>
							</Tooltip.Root>
						{:else}
							<Tooltip.Root>
								<Tooltip.Trigger>
									{#snippet child({ props })}
										<Button
											{...props}
											size="icon-sm"
											onclick={() => connect(p.key)}
											aria-label="Connect"
										>
											<PlugIcon />
										</Button>
									{/snippet}
								</Tooltip.Trigger>
								<Tooltip.Content>Connect</Tooltip.Content>
							</Tooltip.Root>
						{/if}
					</div>
				</div>
			{/each}
		</Card.Root>
		</Tooltip.Provider>
	{/if}
</div>
