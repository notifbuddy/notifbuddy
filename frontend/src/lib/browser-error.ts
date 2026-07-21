// Maps stable API browser-error codes (NOT-37) to user-facing copy.
// Codes come from the backend via /interrupted?code= — never free-form exception text.

const PROVIDER_LABEL: Record<string, string> = {
	slack: 'Slack',
	linear: 'Linear'
};

export function providerLabel(provider: string | null): string {
	if (!provider) return 'this integration';
	return PROVIDER_LABEL[provider] ?? provider;
}

export function messageForCode(code: string | null, provider: string | null): string {
	const name = providerLabel(provider);
	switch (code) {
		case 'not_configured':
			return `${name} isn't set up on this server yet. Ask your admin to configure it.`;
		case 'no_org':
			return `Sign in with an organization before connecting ${name}.`;
		case 'start_failed':
			return `Couldn't start the ${name} connection. Try again in a moment.`;
		case 'oauth_denied':
			return `The ${name} authorization was denied or cancelled.`;
		case 'invalid_state':
			return `This connection link expired or didn't match the browser that started it. Start again from Integrations.`;
		case 'missing_code':
			return `${name} didn't return an authorization code. Start the connection again.`;
		case 'token':
			return `Couldn't secure the ${name} token. Try connecting again.`;
		case 'store':
			return `Connected to ${name}, but saving failed. Try again.`;
		case 'billing_locked':
			return `Your trial has ended. Subscribe to keep connecting integrations.`;
		default:
			return `Something interrupted the ${name} connection. Give it a moment and try again.`;
	}
}

export function titleFor(provider: string | null, code: string | null): string {
	const name = providerLabel(provider);
	if (code === 'billing_locked') return 'Unable to connect integrations';
	return `Unable to connect ${name}`;
}

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
