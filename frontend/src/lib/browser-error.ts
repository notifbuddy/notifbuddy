// Maps interrupt query codes to CTA targets only. Title/message come from the
// backend redirect (NOT-37) — do not re-map copy here.

export function homeHrefFor(code: string | null, provider: string | null): string {
	if (code === 'billing_locked') return '/settings/billing';
	if (provider) return '/settings/integrations';
	return '/';
}

export function ctaLabelFor(code: string | null, provider: string | null): string {
	if (code === 'billing_locked') return 'Go to billing';
	if (provider) return 'Back to integrations';
	return 'Back to the signal';
}
