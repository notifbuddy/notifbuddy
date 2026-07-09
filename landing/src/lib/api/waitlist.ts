import createClient from 'openapi-fetch';
import { PUBLIC_WAITLIST_URL } from '$env/static/public';
import type { paths } from './waitlist-schema';

// Typed client for the standalone waitlist service (waitlist-backend/, its
// own Neon database). Cookieless: no credentials, no session. The base URL is
// baked in at build time — set PUBLIC_WAITLIST_URL to the deployed service's
// URL (infra's `waitlist_url` output) for production builds.
export const waitlistApi = createClient<paths>({
	baseUrl: PUBLIC_WAITLIST_URL
});
