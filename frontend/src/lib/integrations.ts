import { api, apiBaseUrl } from '$lib/api/client';

export type IntegrationStatus = {
	provider: string;
	connected: boolean;
	account?: string;
	connectedBy?: string;
};

export type IntegrationState = {
	configured: boolean;
	integrations: IntegrationStatus[];
};

// Provider display metadata for the UI.
export const PROVIDERS = [
	{ key: 'github', label: 'GitHub', blurb: 'Install the GitHub App to connect your repositories.' },
	{ key: 'slack', label: 'Slack', blurb: 'Authorize Slack so we can post to your workspace.' },
	{ key: 'linear', label: 'Linear', blurb: 'Install the Linear app to sync issues in your workspace.' }
] as const;

// Fetch the current org's integration status. Returns null when unauthenticated.
export async function fetchIntegrationStatus(): Promise<IntegrationState | null> {
	const { data, error } = await api.GET('/integrations/status');
	if (error || !data) return null;
	return data as IntegrationState;
}

// Start a connect flow by full-page navigation to the backend redirect route.
export function connect(provider: string) {
	window.location.href = `${apiBaseUrl}/integrations/${provider}/connect`;
}

// Disconnect a provider; returns the refreshed state (or null on failure).
export async function disconnect(provider: string): Promise<IntegrationState | null> {
	const { data, error } = await api.POST('/integrations/{provider}/disconnect', {
		params: { path: { provider: provider as 'github' | 'slack' | 'linear' } }
	});
	if (error || !data) return null;
	return data as IntegrationState;
}

// Helper: look up one provider's status from the state.
export function statusOf(state: IntegrationState | null, provider: string): IntegrationStatus | undefined {
	return state?.integrations.find((i) => i.provider === provider);
}

export type WebhookEvent = {
	deliveryId: string;
	eventType: string;
	action?: string;
	receivedAt: string;
	payload?: string;
};

// Fetch recent GitHub webhook deliveries for the active org. Returns null when
// unauthenticated.
export async function fetchGithubWebhooks(): Promise<WebhookEvent[] | null> {
	const { data, error } = await api.GET('/integrations/github/webhooks');
	if (error || !data) return null;
	return (data.events ?? []) as WebhookEvent[];
}

// Fetch recent Linear webhook deliveries for the active org. Returns null when
// unauthenticated.
export async function fetchLinearWebhooks(): Promise<WebhookEvent[] | null> {
	const { data, error } = await api.GET('/integrations/linear/webhooks');
	if (error || !data) return null;
	return (data.events ?? []) as WebhookEvent[];
}
