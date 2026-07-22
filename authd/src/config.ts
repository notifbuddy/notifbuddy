// authd configuration — same pattern as the backend: non-sensitive values are
// set literally in a committed YAML file, SENSITIVE values reference an env
// var with `${VAR}` (or `$VAR`), resolved at startup — a referenced-but-unset
// variable is a hard error. Select the file with CONFIG_FILE (default:
// config.local.yaml in the working directory — run from authd/).
import { readFileSync } from 'node:fs';
import { parse } from 'yaml';

export interface Config {
	server: { port: number };
	auth: {
		base_url: string;
		secret: string;
		cookie_domain: string;
		// Empty → Better Auth default "better-auth" (cookie better-auth.session_token).
		// Preview sets e.g. "better-auth-pr-57" so Domain=.notifbuddy.com does not
		// overwrite the prod session cookie.
		cookie_prefix: string;
	};
	database: { url: string };
	cors: { trusted_origins: string[] };
	github: { client_id: string; client_secret: string };
	email: { resend_api_key: string; from: string };
}

function resolveEnvRefs(value: unknown): unknown {
	if (typeof value === 'string') {
		return value.replace(/\$\{(\w+)\}|\$(\w+)/g, (_, braced, bare) => {
			const name = braced ?? bare;
			const v = process.env[name];
			if (v === undefined) throw new Error(`authd: config references unset env var ${name}`);
			return v;
		});
	}
	if (Array.isArray(value)) return value.map(resolveEnvRefs);
	if (value && typeof value === 'object') {
		return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, resolveEnvRefs(v)]));
	}
	return value;
}

const file = process.env.CONFIG_FILE ?? 'config.local.yaml';

export const config = resolveEnvRefs(parse(readFileSync(file, 'utf8'))) as Config;

// Required settings fail at boot, never silently at first use.
if (!config.database?.url) throw new Error('authd: database.url is required');
if (!config.auth?.base_url) throw new Error('authd: auth.base_url is required');
if (!config.auth?.secret) throw new Error('authd: auth.secret is required');
if (!config.github?.client_id || !config.github?.client_secret) {
	throw new Error('authd: github.client_id and github.client_secret are required');
}
if (config.email?.resend_api_key && !config.email?.from) {
	throw new Error('authd: email.from is required when email.resend_api_key is set');
}
