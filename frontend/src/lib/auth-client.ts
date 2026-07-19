// Better Auth browser client — the SPA talks to authd directly for sign-in,
// sign-up, sign-out, and org switching; the Go backend only consumes the
// resulting session cookie. PUBLIC_AUTH_URL must point at authd
// (http://localhost:8787 in dev); like PUBLIC_API_BASE_URL, an unset value is
// a browser-side 500, so it lives in frontend/.env.
import { createAuthClient } from 'better-auth/client';
import { organizationClient } from 'better-auth/client/plugins';
import { PUBLIC_AUTH_URL } from '$env/static/public';

export const authClient = createAuthClient({
	baseURL: PUBLIC_AUTH_URL,
	plugins: [organizationClient()]
});
