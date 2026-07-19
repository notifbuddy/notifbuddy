import createClient from 'openapi-fetch';
import { apiBaseUrl } from '$lib/runtime-config';
import type { paths } from './schema';

// Fully typed API client. Every request/response is typed against the
// generated OpenAPI schema (schema.d.ts) — there are no hand-written
// request or response shapes. Regenerate types with `npm run generate`.
//
// credentials: 'include' sends the HttpOnly `wos_session` cookie on every
// cross-origin request, so the Go API can authenticate the caller. This pairs
// with the credentialed CORS config on the backend (exact allow-origin +
// Access-Control-Allow-Credentials: true).
export const api = createClient<paths>({
	baseUrl: apiBaseUrl,
	credentials: 'include'
});

// Base URL for full-page navigations to the backend's redirect auth routes
// (/auth/login, /auth/logout). These are browser redirects, not fetch calls,
// so they bypass the typed client. Re-exported here because callers have
// always imported it from this module.
export { apiBaseUrl };
