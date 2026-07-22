// authd configuration — shared tree at repo-root config/.
//
// App config:   config/authd/${NB_ENV}.yaml   (default NB_ENV=local)
// Feature flags: config/featureflags/${NB_ENV}.yaml
// Override paths with CONFIG_FILE / FEATUREFLAGS_FILE.
//
// Sensitive values use `${VAR}` env refs; unset refs are a hard error.
import { existsSync, readFileSync } from 'node:fs';
import { parse } from 'yaml';

export interface Config {
	server: { port: number };
	auth: {
		base_url: string;
		secret: string;
		cookie_domain: string;
		// Empty → Better Auth default "better-auth".
		cookie_prefix: string;
	};
	database: { url: string };
	cors: { trusted_origins: string[] };
	github: { client_id: string; client_secret: string };
	email: { resend_api_key: string; from: string };
}

export interface FeatureFlags {
	email_password_login: boolean;
	github_oauth_login: boolean;
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

function resolvePath(area: string, explicit?: string): string {
	if (explicit) return explicit;
	const env = process.env.NB_ENV || 'local';
	const rel = `config/${area}/${env}.yaml`;
	for (const prefix of ['.', '..']) {
		const candidate = `${prefix}/${rel}`;
		if (existsSync(candidate)) return candidate;
	}
	throw new Error(
		`authd: ${rel} not found (set CONFIG_FILE / FEATUREFLAGS_FILE or NB_ENV; tried ./${rel} and ../${rel})`,
	);
}

const configFile = resolvePath('authd', process.env.CONFIG_FILE);
const flagsFile = resolvePath('featureflags', process.env.FEATUREFLAGS_FILE);

export const config = resolveEnvRefs(parse(readFileSync(configFile, 'utf8'))) as Config;
export const featureFlags = parse(readFileSync(flagsFile, 'utf8')) as FeatureFlags;

// Required settings fail at boot, never silently at first use.
if (!config.database?.url) throw new Error('authd: database.url is required');
if (!config.auth?.base_url) throw new Error('authd: auth.base_url is required');
if (!config.auth?.secret) throw new Error('authd: auth.secret is required');

const emailOn = !!featureFlags.email_password_login;
const githubOn = !!featureFlags.github_oauth_login;
if (!emailOn && !githubOn) {
	throw new Error('authd: at least one of email_password_login / github_oauth_login must be true');
}
if (githubOn && (!config.github?.client_id || !config.github?.client_secret)) {
	throw new Error('authd: github.client_id and github.client_secret are required when github_oauth_login is true');
}
if (config.email?.resend_api_key && !config.email?.from) {
	throw new Error('authd: email.from is required when email.resend_api_key is set');
}
