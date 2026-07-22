// Where the SPA finds the API and authd.
//
// The dashboard is a static bundle, so these origins can't simply be baked in
// at build time: a single published image has to serve any self-hosted
// hostname. Resolution order is
//
//   1. window.__notifbuddy, set by the inline marker script in app.html. Our
//      own deploys serve that line inert; the self-hosted dashboard image
//      rewrites it at container start from the chart's values, so one image
//      works for every install. Inline rather than a /config.js file because
//      a blocking request would sit on the critical path of every load,
//      uncacheable, including for deploys that don't need it.
//   2. the build-time PUBLIC_* vars — how our own Cloudflare deploy and local
//      `vite dev` have always worked, unchanged.
//
// An origin that resolves to neither is a hard error in the browser rather
// than a pile of requests to a relative path.
import {
	PUBLIC_API_BASE_URL,
	PUBLIC_AUTH_URL,
	PUBLIC_FEATURE_EMAIL_PASSWORD,
	PUBLIC_FEATURE_GITHUB_OAUTH
} from '$env/static/public';

export type RuntimeConfig = {
	apiBaseUrl?: string;
	authUrl?: string;
	featureEmailPassword?: boolean;
	featureGithubOauth?: boolean;
};

declare global {
	interface Window {
		__notifbuddy?: RuntimeConfig;
	}
}

const runtime: RuntimeConfig =
	(typeof window !== 'undefined' && window.__notifbuddy) || {};

function resolve(key: 'apiBaseUrl' | 'authUrl', buildTime: string): string {
	const url = (runtime[key] ?? '').trim() || (buildTime ?? '').trim();
	if (!url && typeof window !== 'undefined') {
		throw new Error(
			`notifbuddy: ${key} is not configured. Set window.__notifbuddy.${key} ` +
				`(self-hosted: the dashboard values in the Helm chart, which rewrite ` +
				`the marker script in index.html), or build with the matching PUBLIC_* ` +
				`environment variable.`
		);
	}
	return url;
}

function resolveFlag(
	runtimeKey: 'featureEmailPassword' | 'featureGithubOauth',
	buildTime: string | undefined,
	defaultOn: boolean
): boolean {
	const fromWindow = runtime[runtimeKey];
	if (typeof fromWindow === 'boolean') return fromWindow;
	const raw = (buildTime ?? '').trim().toLowerCase();
	if (raw === 'true' || raw === '1') return true;
	if (raw === 'false' || raw === '0') return false;
	return defaultOn;
}

export const apiBaseUrl = resolve('apiBaseUrl', PUBLIC_API_BASE_URL);
export const authUrl = resolve('authUrl', PUBLIC_AUTH_URL);

// Defaults match config/featureflags/local.yaml (GitHub on, email/password off).
export const featureEmailPassword = resolveFlag(
	'featureEmailPassword',
	PUBLIC_FEATURE_EMAIL_PASSWORD,
	false
);
export const featureGithubOauth = resolveFlag(
	'featureGithubOauth',
	PUBLIC_FEATURE_GITHUB_OAUTH,
	true
);
