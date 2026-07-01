<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import RefreshIcon from '@lucide/svelte/icons/refresh-cw';
	import { fetchLinearWebhooks, type WebhookEvent } from '$lib/integrations';

	let events = $state<WebhookEvent[] | null | undefined>(undefined); // undefined = loading
	let expanded = $state<Record<string, boolean>>({});
	let refreshing = $state(false);

	async function load() {
		events = await fetchLinearWebhooks();
	}
	load();

	async function refresh() {
		refreshing = true;
		events = await fetchLinearWebhooks();
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

<div class="flex flex-col gap-6">
	<div class="flex items-start justify-between gap-4">
		<div class="flex flex-col gap-1">
			<h1 class="text-2xl font-semibold tracking-tight">Linear webhooks</h1>
			<p class="text-muted-foreground text-sm">Recent deliveries received for your organization.</p>
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

	{#if events === undefined}
		<!-- Skeleton mirroring the delivery rows. -->
		<ul class="flex flex-col gap-1">
			{#each [0, 1, 2, 3] as i (i)}
				<li class="flex items-center justify-between gap-3 py-2.5">
					<div class="flex items-center gap-2">
						<Skeleton class="h-5 w-28" />
						<Skeleton class="h-4 w-16" />
					</div>
					<Skeleton class="h-4 w-32 shrink-0" />
				</li>
			{/each}
		</ul>
	{:else if events === null}
		<div class="flex flex-col items-start gap-2">
			<p class="text-destructive text-sm">Please sign in first.</p>
			<Button href="/">Go to sign in</Button>
		</div>
	{:else if events.length === 0}
		<p class="text-muted-foreground text-sm">
			No webhooks received yet. Once Linear delivers events for your workspace, they'll appear here.
		</p>
	{:else}
		<ul class="flex flex-col gap-1">
			{#each events as e (e.deliveryId)}
				<li class="py-2.5">
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

	<div>
		<Button variant="ghost" href="/settings/integrations">Back to integrations</Button>
	</div>
</div>
