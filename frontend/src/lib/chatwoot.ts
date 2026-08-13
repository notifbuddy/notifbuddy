import { api } from '$lib/api/client';

type ChatwootUser = {
	email?: string;
	name?: string;
	identifier_hash?: string;
};

type ChatwootSDK = {
	toggle: (state?: 'open' | 'close') => void;
	setUser: (identifier: string, user: ChatwootUser) => void;
	setCustomAttributes: (attributes: Record<string, string>) => void;
	setColorScheme: (darkMode: 'light' | 'dark' | 'auto') => void;
	reset: () => void;
};

declare global {
	interface Window {
		chatwootSettings?: Record<string, unknown>;
		chatwootSDK?: { run: (options: { websiteToken: string; baseUrl: string }) => void };
		$chatwoot?: ChatwootSDK;
	}
}

const SCRIPT_ID = 'chatwoot-sdk';

let started = false;

export function setSupportWidgetTheme(theme: 'light' | 'dark'): void {
	if (typeof window === 'undefined') return;
	window.$chatwoot?.setColorScheme(theme);
}

export async function initSupportWidget(theme: 'light' | 'dark'): Promise<void> {
	if (started || typeof window === 'undefined') return;
	started = true;

	try {
		const { data, error } = await api.GET('/support/chat');
		if (error || !data?.enabled || !data.baseUrl || !data.websiteToken) return;

		window.chatwootSettings = {
			position: 'right',
			type: 'standard',
			darkMode: theme,
			launcherTitle: 'Support',
			enableEmojiPicker: false
		};

		// setUser and setCustomAttributes only exist once the SDK has booted; the
		// widget dispatches chatwoot:ready on window when that has happened.
		window.addEventListener('chatwoot:ready', () => {
			if (!data.identifier) return;
			window.$chatwoot?.setUser(data.identifier, {
				...(data.email ? { email: data.email } : {}),
				...(data.name ? { name: data.name } : {}),
				...(data.identifierHash ? { identifier_hash: data.identifierHash } : {})
			});
			const attributes: Record<string, string> = {};
			if (data.organizationId) attributes.organizationId = data.organizationId;
			if (data.organizationName) attributes.organizationName = data.organizationName;
			if (data.plan) attributes.plan = data.plan;
			if (Object.keys(attributes).length) window.$chatwoot?.setCustomAttributes(attributes);
		});

		if (document.getElementById(SCRIPT_ID)) return;
		const script = document.createElement('script');
		script.id = SCRIPT_ID;
		script.src = `${data.baseUrl}/packs/js/sdk.js`;
		script.defer = true;
		script.async = true;
		script.onload = () =>
			window.chatwootSDK?.run({ websiteToken: data.websiteToken!, baseUrl: data.baseUrl! });
		document.body.appendChild(script);
	} catch {
		return;
	}
}
