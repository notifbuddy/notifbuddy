import { test as base, expect } from '@playwright/test';
import { readFileSync } from 'node:fs';

// ---------------------------------------------------------------------------
// Auth for the dashboard e2e suite.
//
// These tests hit the REAL Go backend (see backend/e2e/docker-compose.e2e.yml).
// The backend authenticates via a sealed WorkOS session cookie whose JWT
// signature it never verifies, so fakeapis forges one offline and writes it to
// the shared volume as session.json. We read that here and seed the cookie into
// the browser context.
//
// Cookie scope: the SPA is served at http://localhost:5173 and the API at
// http://localhost:8080 (the ui container shares the backend's netns). Those are
// different origins (CORS applies) but the SAME site (host "localhost"), so a
// host-only cookie on "localhost" is sent to the API on every credentialed
// fetch. E2E_COOKIE_DOMAIN overrides the host for other setups.
// ---------------------------------------------------------------------------

const SESSION_FILE = process.env.E2E_SESSION_FILE ?? '/certs/session.json';
const COOKIE_DOMAIN = process.env.E2E_COOKIE_DOMAIN ?? 'localhost';

export interface SessionInfo {
	cookie: string;
	userId: string;
	email: string;
	name: string;
	orgId: string;
	orgName: string;
	role: string;
}

export function loadSession(): SessionInfo {
	return JSON.parse(readFileSync(SESSION_FILE, 'utf8')) as SessionInfo;
}

// The forged identity, loaded once. Specs assert against these (e.g. the org
// name shown in the switcher) so the fixtures stay the single source of truth.
export const session: SessionInfo = loadSession();

// Signed-in test: seeds the forged wos_session cookie before each test so the
// browser starts authenticated as the shared e2e admin.
export const test = base.extend({
	context: async ({ context }, use) => {
		await context.addCookies([
			{
				name: 'wos_session',
				value: session.cookie,
				domain: COOKIE_DOMAIN,
				path: '/',
				httpOnly: true,
				secure: false,
				sameSite: 'Lax'
			}
		]);
		await use(context);
	}
});

// Signed-out test: the raw Playwright test, no cookie seeded.
export { base as anonTest, expect };
