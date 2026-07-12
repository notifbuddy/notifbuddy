import { api } from '$lib/api/client';
import type { components } from '$lib/api/schema';

export type BillingStatus = components['schemas']['BillingStatusResponse'];
export type BillingSummary = components['schemas']['BillingSummary'];

// Fetch the active org's billing status. Returns null when unauthenticated or
// billing is unavailable.
export async function fetchBilling(): Promise<BillingStatus | null> {
	const { data, error } = await api.GET('/billing');
	if (error || !data) return null;
	return data as BillingStatus;
}

// Start a Stripe Checkout session for the Pro plan. Returns the URL to
// redirect the browser to, or an error message.
export async function createCheckout(): Promise<{ url?: string; error?: string }> {
	const { data, error } = await api.POST('/billing/checkout');
	if (error) return { error: error.message ?? 'failed to start checkout' };
	if (!data?.url) return { error: 'failed to start checkout' };
	return { url: data.url };
}

// Open the Stripe Customer Portal (card updates, cancellation, invoices).
export async function createPortal(): Promise<{ url?: string; error?: string }> {
	const { data, error } = await api.POST('/billing/portal');
	if (error) return { error: error.message ?? 'failed to open the billing portal' };
	if (!data?.url) return { error: 'failed to open the billing portal' };
	return { url: data.url };
}

// Apply for the free open-source tier. Returns the refreshed billing status,
// or an error message.
export async function submitOssApplication(
	sponsorUrl: string,
	note?: string
): Promise<{ status?: BillingStatus; error?: string }> {
	const { data, error } = await api.POST('/billing/oss-application', {
		body: { sponsorUrl, ...(note ? { note } : {}) }
	});
	if (error) return { error: error.message ?? 'failed to submit the application' };
	return { status: data as BillingStatus };
}

// Days (rounded up, floor 0) until the trial ends.
export function trialDaysLeft(trialEndsAt: string): number {
	const ms = new Date(trialEndsAt).getTime() - Date.now();
	return Math.max(0, Math.ceil(ms / (24 * 60 * 60 * 1000)));
}

// "$9.99" from cents.
export function formatPrice(cents: number): string {
	return `$${(cents / 100).toFixed(2)}`;
}

// Human label for a plan key.
export function planLabel(plan: string): string {
	switch (plan) {
		case 'beta':
			return 'Beta';
		case 'pro':
			return 'Pro';
		case 'oss_free':
			return 'Open Source';
		case 'enterprise':
			return 'Enterprise';
		default:
			return 'Trial';
	}
}
