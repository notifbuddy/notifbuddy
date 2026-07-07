<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import { Badge } from '$lib/components/ui/badge';
	import CheckIcon from '@lucide/svelte/icons/check';
	import HeartIcon from '@lucide/svelte/icons/heart';
	import MailIcon from '@lucide/svelte/icons/mail';
	import CreditCardIcon from '@lucide/svelte/icons/credit-card';
	import { userStore, signIn } from '$lib/user.svelte';

	const user = $derived(userStore.user);

	type Tier = {
		name: string;
		price: string;
		priceNote: string;
		description: string;
		features: string[];
	};

	const tiers: Tier[] = [
		{
			name: 'Open Source',
			price: 'Free',
			priceNote: 'forever',
			description: 'For open-source projects. Apply in-app with a link to your repo or sponsor page.',
			features: [
				'Everything in Pro',
				'Unlimited members',
				'"Sponsored by NotifBuddy" tag on your README',
				'Manual review — usually within a few days'
			]
		},
		{
			name: 'Pro',
			price: '$9.99',
			priceNote: 'per member / month',
			description: 'Bidirectional Linear ↔ Slack sync for teams that live in both.',
			features: [
				'21-day free trial, no card required',
				'Auto-created Slack channels from Linear issues',
				'Comment mirroring in both directions',
				'Seats adjust automatically with your team'
			]
		},
		{
			name: 'Enterprise',
			price: 'Custom',
			priceNote: 'annual agreement',
			description: 'For larger organizations with procurement, security, or invoicing needs.',
			features: ['Everything in Pro', 'Custom terms and invoicing', 'Priority support']
		}
	];
</script>

<svelte:head><title>Pricing — NotifBuddy</title></svelte:head>

<div class="mx-auto flex w-full max-w-5xl flex-col gap-10 px-4 py-16 sm:px-6">
	<div class="flex flex-col items-center gap-3 text-center">
		<h1 class="text-3xl font-semibold tracking-tight sm:text-4xl">Pricing</h1>
		<p class="text-muted-foreground max-w-xl text-sm sm:text-base">
			Start free for 21 days — no card, no sales call. Free forever if you're building open
			source.
		</p>
	</div>

	<div class="grid gap-6 md:grid-cols-3">
		{#each tiers as tier (tier.name)}
			<Card.Root class={tier.name === 'Pro' ? 'border-primary' : ''}>
				<Card.Header>
					<Card.Title class="flex items-center gap-2 text-base">
						{tier.name}
						{#if tier.name === 'Pro'}
							<Badge variant="secondary">21-day free trial</Badge>
						{/if}
					</Card.Title>
					<div class="flex items-baseline gap-1.5">
						<span class="text-3xl font-semibold tracking-tight">{tier.price}</span>
						<span class="text-muted-foreground text-sm">{tier.priceNote}</span>
					</div>
					<Card.Description>{tier.description}</Card.Description>
				</Card.Header>
				<Card.Content class="flex flex-1 flex-col gap-4">
					<ul class="flex flex-col gap-2">
						{#each tier.features as feature (feature)}
							<li class="flex items-start gap-2 text-sm">
								<CheckIcon class="text-muted-foreground mt-0.5 size-4 shrink-0" />
								{feature}
							</li>
						{/each}
					</ul>
				</Card.Content>
				<Card.Footer>
					{#if tier.name === 'Pro'}
						{#if user}
							<Button class="w-full" href="/settings/billing">
								<CreditCardIcon data-icon="inline-start" />
								Manage plan
							</Button>
						{:else}
							<Button class="w-full" onclick={signIn}>Start free trial</Button>
						{/if}
					{:else if tier.name === 'Open Source'}
						{#if user}
							<Button class="w-full" variant="outline" href="/settings/billing">
								<HeartIcon data-icon="inline-start" />
								Apply in billing
							</Button>
						{:else}
							<Button class="w-full" variant="outline" onclick={signIn}>
								<HeartIcon data-icon="inline-start" />
								Sign in to apply
							</Button>
						{/if}
					{:else}
						<Button
							class="w-full"
							variant="outline"
							href="mailto:sales@notifbuddy.com?subject=NotifBuddy%20Enterprise"
						>
							<MailIcon data-icon="inline-start" />
							Contact us
						</Button>
					{/if}
				</Card.Footer>
			</Card.Root>
		{/each}
	</div>

	<p class="text-muted-foreground text-center text-xs">
		Prices exclude tax. Seats are counted from your organization's members and prorated
		automatically.
	</p>
</div>
