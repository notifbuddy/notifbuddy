<script lang="ts">
	import TopNav from './top-nav.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import ClockIcon from '@lucide/svelte/icons/clock';
	import LockIcon from '@lucide/svelte/icons/lock';
	import CreditCardIcon from '@lucide/svelte/icons/credit-card';
	import { page } from '$app/state';
	import { userStore } from '$lib/user.svelte';
	import { trialDaysLeft } from '$lib/billing';
	import type { Snippet } from 'svelte';

	let { children }: { children: Snippet } = $props();

	const billing = $derived(userStore.user?.billing);
	const path = $derived(page.url.pathname);

	// Billing/profile stay reachable when locked — that's where users go to fix it.
	const lockExempt = $derived(
		path.startsWith('/settings/billing') || path.startsWith('/settings/profile')
	);
	const locked = $derived(Boolean(billing?.locked) && !lockExempt);
	const showTrialBanner = $derived(
		billing?.plan === 'trial' && !billing.locked && !path.startsWith('/settings/billing')
	);
</script>

<!-- Top-nav shell: a sticky header with the org switcher, primary nav, and the
     profile menu; page content renders full-view inside a centered container.
     Sub-pages surface as secondary tabs on their landing page, not in the bar. -->
<div class="flex min-h-svh flex-col">
	<TopNav />
	{#if showTrialBanner && billing}
		<div class="border-b">
			<div
				class="text-muted-foreground mx-auto flex w-full max-w-6xl items-center gap-2 px-4 py-2 text-sm sm:px-6"
			>
				<ClockIcon class="size-4 shrink-0" />
				<span>
					<span class="text-foreground font-medium">
						{trialDaysLeft(billing.trialEndsAt)} days
					</span>
					left in your free trial.
				</span>
				<a href="/settings/billing" class="text-foreground font-medium underline underline-offset-4">
					Choose a plan
				</a>
			</div>
		</div>
	{/if}
	<main class="mx-auto w-full max-w-6xl flex-1 px-4 py-6 sm:px-6">
		{#if locked}
			<!-- Billing lock: the trial ended and there's no subscription. The
			     backend also 402s mutations, so this is UX, not the enforcement. -->
			<div class="flex flex-1 items-center justify-center py-16">
				<Card.Root class="w-full max-w-md">
					<Card.Header>
						<Card.Title class="flex items-center gap-2">
							<LockIcon class="size-4" />
							Your trial has ended
						</Card.Title>
						<Card.Description>
							Syncing between Linear and Slack is paused. Subscribe to Pro — or apply for the
							free open-source tier — to pick up right where you left off.
						</Card.Description>
					</Card.Header>
					<Card.Content>
						<Button href="/settings/billing">
							<CreditCardIcon data-icon="inline-start" />
							Go to billing
						</Button>
					</Card.Content>
				</Card.Root>
			</div>
		{:else}
			{@render children()}
		{/if}
	</main>
</div>
