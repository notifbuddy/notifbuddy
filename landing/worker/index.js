// The landing Worker: static assets first, one API route.
//
// Requests that match a file in build/ are served as static assets before
// this code runs. Everything else lands here: POST /waitlist is proxied
// same-origin to the Cloud Run waitlist service (WAITLIST_ORIGIN var in
// wrangler.jsonc), so the browser never makes a cross-origin call and CORS
// never applies in production. Anything else falls through to the asset
// handler for its 404. Local dev bypasses all of this (PUBLIC_WAITLIST_URL
// points straight at the service on :8081).
export default {
	async fetch(request, env) {
		const url = new URL(request.url);
		if (url.pathname === '/waitlist' && request.method === 'POST') {
			return fetch(`${env.WAITLIST_ORIGIN}/waitlist`, request);
		}
		return env.ASSETS.fetch(request);
	}
};
