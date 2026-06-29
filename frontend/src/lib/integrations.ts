import { api, apiBaseUrl } from '$lib/api/client';

// An integration can be connected at the org-wide "workspace" level (install /
// bot) or per "user" level (the caller's own OAuth token, used to act as them).
export type Level = 'workspace' | 'user';

export type IntegrationStatus = {
	provider: string;
	level: Level;
	connected: boolean;
	account?: string;
	connectedBy?: string;
};

export type IntegrationState = {
	configured: boolean;
	integrations: IntegrationStatus[];
};

// Provider display metadata for the UI. Blurbs are per level: workspace describes
// the org-wide install, user describes the personal connection used for sync.
export const PROVIDERS = [
	{
		key: 'github',
		label: 'GitHub',
		workspaceBlurb: 'Install the GitHub App to connect your repositories.',
		userBlurb: 'Connect your GitHub account so actions sync as you.'
	},
	{
		key: 'slack',
		label: 'Slack',
		workspaceBlurb: 'Authorize Slack so we can post to your workspace.',
		userBlurb: 'Connect your Slack account so messages sync as you.'
	},
	{
		key: 'linear',
		label: 'Linear',
		workspaceBlurb: 'Install the Linear app to sync issues in your workspace.',
		userBlurb: 'Connect your Linear account so actions sync as you.'
	}
] as const;

// Fetch the current org's integration status. Returns null when unauthenticated.
export async function fetchIntegrationStatus(): Promise<IntegrationState | null> {
	const { data, error } = await api.GET('/integrations/status');
	if (error || !data) return null;
	return data as IntegrationState;
}

// Start a connect flow by full-page navigation to the backend redirect route.
// level defaults to workspace; pass 'user' for a personal connection.
export function connect(provider: string, level: Level = 'workspace') {
	const q = level === 'user' ? '?level=user' : '';
	window.location.href = `${apiBaseUrl}/integrations/${provider}/connect${q}`;
}

// Disconnect a provider at a level; returns the refreshed state (or null on failure).
export async function disconnect(
	provider: string,
	level: Level = 'workspace'
): Promise<IntegrationState | null> {
	const { data, error } = await api.POST('/integrations/{provider}/disconnect', {
		params: {
			path: { provider: provider as 'github' | 'slack' | 'linear' },
			query: { level }
		}
	});
	if (error || !data) return null;
	return data as IntegrationState;
}

// Helper: look up one provider's status at a level from the state.
export function statusOf(
	state: IntegrationState | null,
	provider: string,
	level: Level = 'workspace'
): IntegrationStatus | undefined {
	return state?.integrations.find((i) => i.provider === provider && i.level === level);
}

export type WebhookEvent = {
	deliveryId: string;
	eventType: string;
	action?: string;
	receivedAt: string;
	payload?: string;
};

// ---- Linear settings ----

export type LinearSettings = {
	creationMode: 'status' | 'manual';
	triggerStatus?: string;
	nameTemplate?: string;
	conditionExpr?: string;
	autoAddBots: string[];
};

export type SampleEvent = { id: string; label: string; raw: string };

export type LinearSettingsState = {
	connected: boolean;
	settings: LinearSettings;
	sampleEvents: SampleEvent[];
};

export type TemplateTestResult = { name: string; conditionResult: boolean; error?: string };

// Fetch the org's Linear settings + connection state + sample events.
export async function fetchLinearSettings(): Promise<LinearSettingsState | null> {
	const { data, error } = await api.GET('/integrations/linear/settings');
	if (error || !data) return null;
	return data as LinearSettingsState;
}

// Save the org's Linear settings; returns the refreshed state or null on error.
export async function saveLinearSettings(
	settings: LinearSettings
): Promise<LinearSettingsState | null> {
	const { data, error } = await api.PUT('/integrations/linear/settings', { body: settings });
	if (error || !data) return null;
	return data as LinearSettingsState;
}

// Test a name template + condition against a sample (by id) or a pasted event.
export async function testLinearTemplate(req: {
	nameTemplate?: string;
	condition?: string;
	sampleId?: string;
	event?: string;
}): Promise<TemplateTestResult | null> {
	const { data, error } = await api.POST('/integrations/linear/settings/test', { body: req });
	if (error || !data) return null;
	return data as TemplateTestResult;
}

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
