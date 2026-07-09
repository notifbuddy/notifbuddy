// Cloudflare Pages Function: same-origin proxy for the waitlist form.
//
// The landing page posts to /waitlist on its own domain; this forwards the
// request to the Cloud Run waitlist service. WAITLIST_ORIGIN is set on the
// Pages project by infra (infra) from the Cloud Run output,
// so the browser never makes a cross-origin call and CORS never applies in
// production. Local dev bypasses this entirely (PUBLIC_WAITLIST_URL points
// straight at the service on :8081).
export async function onRequestPost({ request, env }) {
	const url = new URL(request.url);
	return fetch(`${env.WAITLIST_ORIGIN}${url.pathname}`, request);
}
