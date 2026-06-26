<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import RefreshIcon from '@lucide/svelte/icons/refresh-cw';
	import { fetchGithubWebhooks, type WebhookEvent } from '$lib/integrations';

	let events = $state<WebhookEvent[] | null | undefined>(undefined); // undefined = loading
	let expanded = $state<Record<string, boolean>>({});
	let refreshing = $state(false);

	async function load() {
		events = await fetchGithubWebhooks();
	}
	load();

	async function refresh() {
		refreshing = true;
		events = await fetchGithubWebhooks();
		refreshing = false;
	}

	function toggle(id: string) {
		expanded[id] = !expanded[id];
	}

	function pretty(payload?: string): string {
		if (!payload) return '';
		try {
			return JSON.stringify(JSON.parse(payload), null, 2);
		} catch {
			return payload;
		}
	}

	function when(iso: string): string {
		const d = new Date(iso);
		return isNaN(d.getTime()) ? iso : d.toLocaleString();
	}
</script>

<main class="flex min-h-svh items-start justify-center p-6">
	<Card.Root class="w-full max-w-2xl">
		<Card.Header>
			<div class="flex items-center justify-between">
				<div>
					<Card.Title>GitHub webhooks</Card.Title>
					<Card.Description>Recent deliveries received for your organization.</Card.Description>
				</div>
				<Button variant="outline" size="sm" onclick={refresh} disabled={refreshing}>
					{#if refreshing}
						<LoaderIcon data-icon="inline-start" class="animate-spin" />
					{:else}
						<RefreshIcon data-icon="inline-start" />
					{/if}
					Refresh
				</Button>
			</div>
		</Card.Header>
		<Card.Content class="flex flex-col gap-3">
			{#if events === undefined}
				<p class="text-muted-foreground flex items-center gap-2 text-sm">
					<LoaderIcon class="animate-spin" /> Loading…
				</p>
			{:else if events === null}
				<p class="text-destructive text-sm">Please sign in first.</p>
				<Button href="/">Go to sign in</Button>
			{:else if events.length === 0}
				<p class="text-muted-foreground text-sm">
					No webhooks received yet. Once GitHub delivers events for your installation, they'll
					appear here.
				</p>
			{:else}
				<ul class="flex flex-col divide-y">
					{#each events as e (e.deliveryId)}
						<li class="py-2">
							<button
								class="flex w-full items-center justify-between gap-3 text-left"
								onclick={() => toggle(e.deliveryId)}
							>
								<span class="flex items-center gap-2">
									<span class="bg-secondary rounded px-2 py-0.5 font-mono text-xs">{e.eventType}</span>
									{#if e.action}
										<span class="text-muted-foreground text-xs">{e.action}</span>
									{/if}
								</span>
								<span class="text-muted-foreground shrink-0 text-xs">{when(e.receivedAt)}</span>
							</button>
							{#if expanded[e.deliveryId]}
								<div class="mt-2 flex flex-col gap-1">
									<span class="text-muted-foreground font-mono text-[11px]">
										delivery {e.deliveryId}
									</span>
									<pre
										class="bg-muted max-h-80 overflow-auto rounded-md p-3 text-xs">{pretty(e.payload)}</pre>
								</div>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}

			<Button variant="ghost" href="/settings/integrations">Back to integrations</Button>
		</Card.Content>
	</Card.Root>
</main>
