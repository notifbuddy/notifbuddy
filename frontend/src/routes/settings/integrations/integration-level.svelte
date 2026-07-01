<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import * as AlertDialog from '$lib/components/ui/alert-dialog';
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
		type Level,
		type IntegrationState
	} from '$lib/integrations';

	// Which level this page manages: 'workspace' (org-wide) or 'user' (personal).
	let { level }: { level: Level } = $props();

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
		const next = await disconnect(provider, level);
		busy = null;
		if (next) intg = next;
	}

	const icon = (key: string) =>
		key === 'github' ? SiGithub : key === 'linear' ? SiLinear : SlackIcon;

	const blurbOf = (p: (typeof PROVIDERS)[number]) =>
		level === 'user' ? p.userBlurb : p.workspaceBlurb;

	// Webhook views are workspace-scoped (org deliveries), so only shown there.
	const webhooksHref = (key: string) =>
		level !== 'workspace'
			? ''
			: key === 'github'
				? '/settings/integrations/webhooks'
				: key === 'linear'
					? '/settings/integrations/linear-webhooks'
					: '';
</script>

{#if intg === undefined}
	<!-- Skeleton mirroring the connection list. -->
	<div class="flex flex-col gap-2">
		{#each [0, 1] as i (i)}
			<div class="flex flex-wrap items-center gap-4 py-2">
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
	</div>
{:else if intg === null}
	<div class="flex flex-col items-start gap-2">
		<p class="text-destructive text-sm">Please sign in first.</p>
		<Button href="/">Go to sign in</Button>
	</div>
{:else if intg.configured === false}
	<p class="text-muted-foreground text-sm">Integrations aren't configured on the server yet.</p>
{:else}
	<Tooltip.Provider delayDuration={200}>
		<div class="flex flex-col gap-2">
			{#each PROVIDERS as p (p.key)}
				{@const s = statusOf(intg, p.key, level)}
				{@const Icon = icon(p.key)}
				{@const hooks = webhooksHref(p.key)}
				<div class="flex flex-wrap items-center gap-4 py-2">
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
								{blurbOf(p)}
							{/if}
						</p>
					</div>
					<div class="flex shrink-0 items-center gap-1">
						{#if s?.connected}
							{#if hooks}
								<Tooltip.Root>
									<Tooltip.Trigger>
										{#snippet child({ props })}
											<Button
												{...props}
												variant="ghost"
												size="icon-sm"
												href={hooks}
												aria-label="Webhooks"
											>
												<WebhookIcon />
											</Button>
										{/snippet}
									</Tooltip.Trigger>
									<Tooltip.Content>Webhooks</Tooltip.Content>
								</Tooltip.Root>
							{/if}
							<AlertDialog.Root>
								<Tooltip.Root>
									<Tooltip.Trigger>
										{#snippet child({ props: tipProps })}
											<AlertDialog.Trigger>
												{#snippet child({ props: dialogProps })}
													<Button
														{...tipProps}
														{...dialogProps}
														variant="ghost"
														size="icon-sm"
														disabled={busy === p.key}
														aria-label="Disconnect"
													>
														{#if busy === p.key}
															<LoaderIcon class="animate-spin" />
														{:else}
															<UnplugIcon />
														{/if}
													</Button>
												{/snippet}
											</AlertDialog.Trigger>
										{/snippet}
									</Tooltip.Trigger>
									<Tooltip.Content>Disconnect</Tooltip.Content>
								</Tooltip.Root>
								<AlertDialog.Content>
									<AlertDialog.Header>
										<AlertDialog.Title>Disconnect {p.label}?</AlertDialog.Title>
										<AlertDialog.Description>
											This removes the {level === 'user' ? 'personal' : 'workspace'} connection to
											{p.label}. Any sync that relies on it will stop until you reconnect.
										</AlertDialog.Description>
									</AlertDialog.Header>
									<AlertDialog.Footer>
										<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
										<AlertDialog.Action variant="destructive" onclick={() => doDisconnect(p.key)}>
											Disconnect
										</AlertDialog.Action>
									</AlertDialog.Footer>
								</AlertDialog.Content>
							</AlertDialog.Root>
						{:else}
							<Tooltip.Root>
								<Tooltip.Trigger>
									{#snippet child({ props })}
										<Button
											{...props}
											variant="ghost"
											size="icon-sm"
											onclick={() => connect(p.key, level)}
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
		</div>
	</Tooltip.Provider>
{/if}
