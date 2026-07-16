<script lang="ts">
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import { Input } from '$lib/components/ui/input';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Badge } from '$lib/components/ui/badge';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import * as Field from '$lib/components/ui/field';
	import * as Tooltip from '$lib/components/ui/tooltip';
	import LoaderIcon from '@lucide/svelte/icons/loader-circle';
	import CreditCardIcon from '@lucide/svelte/icons/credit-card';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import HeartIcon from '@lucide/svelte/icons/heart';
	import MailIcon from '@lucide/svelte/icons/mail';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import CheckIcon from '@lucide/svelte/icons/check';
	import { page } from '$app/state';
	import { userStore } from '$lib/user.svelte';
	import { fetchMembers, type Member } from '$lib/organization';
	import {
		fetchBilling,
		createCheckout,
		createPortal,
		submitOssApplication,
		trialDaysLeft,
		formatPrice,
		planLabel,
		type BillingStatus
	} from '$lib/billing';

	const org = $derived(userStore.activeOrg);
	const isAdmin = $derived(userStore.user?.role === 'admin');

	// Tooltip body for actions non-admins can see but not use.
	const adminOnlyMsg = 'Only admins can manage billing';

	let billing = $state<BillingStatus | null | undefined>(undefined);
	let members = $state<Member[] | null | undefined>(undefined);

	let redirecting = $state(false);
	let actionError = $state<string | null>(null);

	let sponsorUrl = $state('');
	let sponsorNote = $state('');
	let applying = $state(false);
	let applyMsg = $state<string | null>(null);

	// Free OSS usage requires this sponsor tag on the project's README/website;
	// reviewers check for it before approving.
	const sponsorBadge =
		'[![Sponsored by NotifBuddy](https://img.shields.io/badge/sponsored%20by-NotifBuddy-5e6ad2)](https://notifbuddy.com)';
	let badgeCopied = $state(false);

	async function copyBadge() {
		await navigator.clipboard.writeText(sponsorBadge);
		badgeCopied = true;
		setTimeout(() => (badgeCopied = false), 2000);
	}

	// ?checkout=success|cancel is a one-shot signal from the Stripe redirect.
	// Consume it once and strip it from the URL immediately, so a reload (or a
	// bookmarked URL) doesn't replay stale checkout state.
	const checkoutParam = page.url.searchParams.get('checkout');
	if (checkoutParam !== null) {
		const url = new URL(window.location.href);
		url.searchParams.delete('checkout');
		url.searchParams.delete('session_id');
		history.replaceState(history.state, '', url);
	}
	let activating = $state(checkoutParam === 'success');

	async function load() {
		billing = await fetchBilling();
	}
	load();
	fetchMembers().then((m) => (members = m));

	// The success redirect can land before Stripe's webhook has flipped the
	// plan; poll /billing a bounded number of times so "Activating…" resolves
	// without a manual reload. A plain loop, not an $effect — polling mutates
	// `billing`, and an effect reading it would re-run and reset its own counter.
	if (checkoutParam === 'success') {
		(async () => {
			for (let tries = 0; tries < 5 && !billing?.subscribed; tries++) {
				await new Promise((r) => setTimeout(r, 2000));
				await load();
			}
			activating = false;
			if (billing?.subscribed) userStore.load(true); // refresh shell banner/lock
		})();
	}

	async function subscribe() {
		redirecting = true;
		actionError = null;
		const { url, error } = await createCheckout();
		if (!url) {
			redirecting = false;
			actionError = error ?? 'Could not start checkout.';
			return;
		}
		window.location.href = url;
	}

	async function openPortal() {
		redirecting = true;
		actionError = null;
		const { url, error } = await createPortal();
		if (!url) {
			redirecting = false;
			actionError = error ?? 'Could not open the billing portal.';
			return;
		}
		window.location.href = url;
	}

	async function applyOss(e: SubmitEvent) {
		e.preventDefault();
		applying = true;
		applyMsg = null;
		const { status, error } = await submitOssApplication(
			sponsorUrl.trim(),
			sponsorNote.trim() || undefined
		);
		applying = false;
		if (!status) {
			applyMsg = error ?? 'Could not submit the application.';
			return;
		}
		billing = status;
		applyMsg = 'Application submitted — we review these by hand and will be in touch.';
	}

	const planBadge = (b: BillingStatus): 'secondary' | 'outline' | 'destructive' =>
		b.locked ? 'destructive' : b.plan === 'trial' ? 'outline' : 'secondary';

	const seatCount = $derived(members?.length ?? billing?.seats ?? 1);
	const monthlyTotal = $derived(
		billing ? formatPrice(seatCount * billing.priceCentsPerSeat) : ''
	);
</script>

<div class="flex flex-col gap-6">
	<div class="flex flex-col gap-1">
		<h1 class="text-2xl font-semibold tracking-tight">Billing</h1>
		<p class="text-muted-foreground text-sm">
			{#if org}Plan and payment for {org.name}.{:else}Plan and payment.{/if}
		</p>
	</div>

	<!-- Current plan -->
	<Card.Root>
		<Card.Header>
			<Card.Title class="flex items-center gap-2 text-base">
				Plan
				{#if billing}
					<Badge variant={planBadge(billing)}>
						{#if billing.locked}
							Locked
						{:else if billing.plan === 'trial'}
							Trial — {trialDaysLeft(billing.trialEndsAt)} days left
						{:else}
							{planLabel(billing.plan)}
						{/if}
					</Badge>
				{/if}
			</Card.Title>
			<Card.Description>
				{#if billing?.plan === 'beta'}
					NotifBuddy is free while it's in beta — every feature, no card, no clock.
				{:else}
					Pro is {billing ? formatPrice(billing.priceCentsPerSeat) : '$9.99'} per member per month.
					Every new organization starts with a 21-day free trial — no card needed.
				{/if}
			</Card.Description>
		</Card.Header>
		<Card.Content class="flex flex-col gap-4">
			{#if billing === undefined}
				<div class="flex flex-col gap-3">
					<Skeleton class="h-4 w-64" />
					<Skeleton class="h-9 w-40" />
				</div>
			{:else if billing === null}
				<p class="text-destructive text-sm">Couldn't load billing. Please sign in again.</p>
			{:else}
				{#if activating}
					<p class="text-muted-foreground flex items-center gap-2 text-sm">
						<LoaderIcon class="size-4 animate-spin" />
						Activating your subscription…
					</p>
				{:else if checkoutParam === 'cancel' && !billing.subscribed}
					<p class="text-muted-foreground text-sm">Checkout was cancelled — no charge was made.</p>
				{/if}

				{#if billing.subscribed}
					<p class="text-sm">
						{seatCount}
						{seatCount === 1 ? 'member' : 'members'} × {formatPrice(billing.priceCentsPerSeat)}/mo
						= <span class="font-medium">{monthlyTotal}/mo</span>
						{#if billing.stripeStatus === 'past_due'}
							<Badge variant="destructive" class="ms-2">Payment past due</Badge>
						{/if}
					</p>
					<div>
						<Tooltip.Provider delayDuration={200}>
							<Tooltip.Root>
								<Tooltip.Trigger>
									{#snippet child({ props })}
										<span {...props} class="inline-block">
											<Button variant="outline" onclick={openPortal} disabled={redirecting || !isAdmin}>
												{#if redirecting}
													<LoaderIcon data-icon="inline-start" class="animate-spin" />
													Opening…
												{:else}
													<ExternalLinkIcon data-icon="inline-start" />
													Manage billing
												{/if}
											</Button>
										</span>
									{/snippet}
								</Tooltip.Trigger>
								{#if !isAdmin}
									<Tooltip.Content>{adminOnlyMsg}</Tooltip.Content>
								{/if}
							</Tooltip.Root>
						</Tooltip.Provider>
					</div>
					<p class="text-muted-foreground text-xs">
						Card, invoices, and cancellation are handled in the Stripe portal. Seats adjust
						automatically as members join or leave.
					</p>
				{:else if billing.plan === 'beta'}
					<p class="text-muted-foreground text-sm">
						You're on the free beta plan. Paid plans arrive after the beta — with plenty of
						notice before anything changes.
					</p>
				{:else if billing.plan === 'oss_free'}
					<p class="text-muted-foreground text-sm">
						This organization is on the free open-source tier. Thanks for building in the open ♥
					</p>
				{:else if billing.plan === 'enterprise'}
					<p class="text-muted-foreground text-sm">
						This organization is on an enterprise agreement.
					</p>
				{:else}
					<p class="text-sm">
						{#if billing.locked}
							Your trial has ended. Subscribe to keep syncing Linear and Slack.
						{:else}
							Subscribing now covers {seatCount}
							{seatCount === 1 ? 'member' : 'members'} at {monthlyTotal}/mo.
						{/if}
					</p>
					<div>
						<Tooltip.Provider delayDuration={200}>
							<Tooltip.Root>
								<Tooltip.Trigger>
									{#snippet child({ props })}
										<span {...props} class="inline-block">
											<Button onclick={subscribe} disabled={redirecting || !isAdmin}>
												{#if redirecting}
													<LoaderIcon data-icon="inline-start" class="animate-spin" />
													Redirecting…
												{:else}
													<CreditCardIcon data-icon="inline-start" />
													Subscribe to Pro
												{/if}
											</Button>
										</span>
									{/snippet}
								</Tooltip.Trigger>
								{#if !isAdmin}
									<Tooltip.Content>{adminOnlyMsg}</Tooltip.Content>
								{/if}
							</Tooltip.Root>
						</Tooltip.Provider>
					</div>
				{/if}
				{#if actionError}<p class="text-destructive text-sm">{actionError}</p>{/if}
			{/if}
		</Card.Content>
	</Card.Root>

	<!-- Open-source free tier -->
	{#if billing && !billing.subscribed && billing.plan !== 'oss_free' && billing.plan !== 'enterprise' && billing.plan !== 'beta'}
		<Card.Root>
			<Card.Header>
				<Card.Title class="flex items-center gap-2 text-base">
					<HeartIcon class="size-4" />
					Free for open source
					{#if billing.ossApplicationStatus === 'pending'}
						<Badge variant="outline">Application pending</Badge>
					{:else if billing.ossApplicationStatus === 'rejected'}
						<Badge variant="destructive">Not approved</Badge>
					{/if}
				</Card.Title>
				<Card.Description>
					Maintaining an open-source project? NotifBuddy is free forever for open-source
					organizations. Add the "Sponsored by NotifBuddy" tag to your README, link
					your repo or sponsorship page below, and we'll review it by hand.
				</Card.Description>
			</Card.Header>
			<Card.Content class="flex flex-col gap-4">
				<!-- The required sponsor tag, ready to paste into a README. -->
				<div class="flex flex-col gap-1.5">
					<span class="text-sm font-medium">Sponsor tag (required on your README)</span>
					<div class="bg-muted flex items-center gap-2 rounded-md border px-3 py-2">
						<code class="min-w-0 flex-1 truncate font-mono text-xs">{sponsorBadge}</code>
						<Tooltip.Provider>
							<Tooltip.Root>
								<Tooltip.Trigger>
									{#snippet child({ props })}
										<Button
											{...props}
											variant="ghost"
											size="icon"
											class="size-7 shrink-0"
											onclick={copyBadge}
											aria-label="Copy sponsor tag markdown"
										>
											{#if badgeCopied}
												<CheckIcon class="size-3.5" />
											{:else}
												<CopyIcon class="size-3.5" />
											{/if}
										</Button>
									{/snippet}
								</Tooltip.Trigger>
								<Tooltip.Content>
									{badgeCopied ? 'Copied' : 'Copy markdown'}
								</Tooltip.Content>
							</Tooltip.Root>
						</Tooltip.Provider>
					</div>
				</div>

				{#if billing.ossApplicationStatus === 'pending'}
					<p class="text-muted-foreground text-sm">
						Your application is in review — we'll check for the sponsor tag on your project.
						Your trial keeps running in the meantime.
					</p>
				{:else}
					<form class="flex flex-col gap-4" onsubmit={applyOss}>
						<Field.FieldGroup>
							<Field.Field>
								<Field.FieldLabel for="sponsor-url">Open-source repo or sponsor page</Field.FieldLabel>
								<Input
									id="sponsor-url"
									type="url"
									placeholder="https://github.com/your-org/your-project"
									bind:value={sponsorUrl}
									disabled={applying}
									required
								/>
							</Field.Field>
							<Field.Field>
								<Field.FieldLabel for="sponsor-note">Anything we should know? (optional)</Field.FieldLabel>
								<Textarea
									id="sponsor-note"
									rows={3}
									placeholder="What the project is, who uses it…"
									bind:value={sponsorNote}
									disabled={applying}
								/>
							</Field.Field>
						</Field.FieldGroup>
						<div>
							<Tooltip.Provider delayDuration={200}>
								<Tooltip.Root>
									<Tooltip.Trigger>
										{#snippet child({ props })}
											<span {...props} class="inline-block">
												<Button
													type="submit"
													variant="outline"
													disabled={applying || sponsorUrl.trim() === '' || !isAdmin}
												>
													{#if applying}
														<LoaderIcon data-icon="inline-start" class="animate-spin" />
														Submitting…
													{:else}
														<HeartIcon data-icon="inline-start" />
														Apply
													{/if}
												</Button>
											</span>
										{/snippet}
									</Tooltip.Trigger>
									{#if !isAdmin}
										<Tooltip.Content>{adminOnlyMsg}</Tooltip.Content>
									{/if}
								</Tooltip.Root>
							</Tooltip.Provider>
						</div>
					</form>
				{/if}
				{#if applyMsg}<p class="text-muted-foreground text-sm">{applyMsg}</p>{/if}
			</Card.Content>
		</Card.Root>
	{/if}

	<!-- Enterprise -->
	<Card.Root>
		<Card.Header>
			<Card.Title class="text-base">Enterprise</Card.Title>
			<Card.Description>
				Need SSO enforcement, custom terms, or invoicing? Talk to us.
			</Card.Description>
		</Card.Header>
		<Card.Content>
			<Button variant="outline" href="mailto:sales@notifbuddy.com?subject=NotifBuddy%20Enterprise">
				<MailIcon data-icon="inline-start" />
				Contact us
			</Button>
		</Card.Content>
	</Card.Root>
</div>
