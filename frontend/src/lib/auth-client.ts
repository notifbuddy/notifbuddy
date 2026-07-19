// Better Auth browser client — the SPA talks to authd directly for sign-in,
// sign-up, sign-out, and org switching; the Go backend only consumes the
// resulting session cookie. The authd origin comes from runtime-config
// (http://localhost:8787 in dev, via frontend/.env); an unresolvable value
// throws there rather than failing later as an opaque fetch error.
import { createAuthClient } from 'better-auth/client';
import { organizationClient } from 'better-auth/client/plugins';
import { authUrl } from '$lib/runtime-config';

export const authClient = createAuthClient({
	baseURL: authUrl,
	plugins: [organizationClient()]
});
