import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import tailwindcss from '@tailwindcss/vite';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig, loadEnv } from 'vite';

/** Minimal YAML map loader for our flat config/*.yaml files (no nested maps). */
function loadFlatYaml(path: string): Record<string, string | boolean> {
	const out: Record<string, string | boolean> = {};
	for (const raw of readFileSync(path, 'utf8').split('\n')) {
		const line = raw.replace(/#.*$/, '').trim();
		if (!line) continue;
		const m = line.match(/^([A-Za-z0-9_]+):\s*(.*)$/);
		if (!m) continue;
		let v = m[2].trim();
		if (v === 'true' || v === 'false') {
			out[m[1]] = v === 'true';
			continue;
		}
		if ((v.startsWith('"') && v.endsWith('"')) || (v.startsWith("'") && v.endsWith("'"))) {
			v = v.slice(1, -1);
		}
		out[m[1]] = v;
	}
	return out;
}

function resolveConfigFile(area: string, envName: string): string {
	const rel = `config/${area}/${envName}.yaml`;
	for (const root of [resolve('..'), resolve('.')]) {
		const candidate = resolve(root, rel);
		if (existsSync(candidate)) return candidate;
	}
	throw new Error(`dashboard vite: missing ${rel}`);
}

/** True only when unset — empty string means "explicitly empty" (Helm bake). */
function unset(...vals: Array<string | undefined>): boolean {
	return vals.every((v) => v === undefined);
}

export default defineConfig(({ mode }) => {
	// Prefer explicit PUBLIC_* from the environment (CI / Helm bake). Otherwise
	// load config/dashboard + featureflags for NB_ENV (default local).
	// Empty string counts as set (Helm leaves URLs empty for runtime inject).
	const fileEnv = loadEnv(mode, process.cwd(), '');
	const nbEnv = process.env.NB_ENV || fileEnv.NB_ENV || 'local';

	if (unset(process.env.PUBLIC_API_BASE_URL, fileEnv.PUBLIC_API_BASE_URL)) {
		const dash = loadFlatYaml(resolveConfigFile('dashboard', nbEnv));
		process.env.PUBLIC_API_BASE_URL = String(dash.api_base_url ?? '');
		process.env.PUBLIC_AUTH_URL = String(dash.auth_url ?? '');
	}
	if (unset(process.env.PUBLIC_FEATURE_EMAIL_PASSWORD, fileEnv.PUBLIC_FEATURE_EMAIL_PASSWORD)) {
		const flags = loadFlatYaml(resolveConfigFile('featureflags', nbEnv));
		process.env.PUBLIC_FEATURE_EMAIL_PASSWORD = flags.email_password_login ? 'true' : 'false';
		process.env.PUBLIC_FEATURE_GITHUB_OAUTH = flags.github_oauth_login ? 'true' : 'false';
	}

	return {
		plugins: [
			tailwindcss(),
			sveltekit({
				compilerOptions: {
					runes: ({ filename }) =>
						filename.split(/[/\\]/).includes('node_modules') ? undefined : true
				},
				adapter: adapter({ fallback: 'index.html' }),
				alias: { $shared: '../shared' },
				files: { errorTemplate: '../shared/templates/error.html' }
			})
		],
		server: { fs: { allow: ['..'] } }
	};
});
