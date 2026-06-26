// SPA configuration. We keep SvelteKit as a pure client-side app: the Go
// backend owns the API, and the browser calls it directly via the generated
// openapi-fetch client. Disabling SSR + prerendering the fallback page lets
// adapter-static emit a single index.html SPA shell.
export const ssr = false;
export const prerender = true;
