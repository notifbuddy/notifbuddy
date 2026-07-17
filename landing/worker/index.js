// The landing Worker: static assets only.
//
// Requests that match a file in build/ are served as static assets before
// this code runs. Everything else falls through to the asset handler, which
// serves the prerendered 404 page (see wrangler.jsonc).
export default {
	async fetch(request, env) {
		return env.ASSETS.fetch(request);
	}
};
