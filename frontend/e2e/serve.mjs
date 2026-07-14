// Minimal static file server for the built SPA, with single-page-app fallback:
// unknown paths (client-side routes like /dashboard/linear) serve index.html so
// the SvelteKit client router can take over. Zero dependencies — Playwright's
// webServer launches this and waits for the port.
import { createServer } from 'node:http';
import { readFile, stat } from 'node:fs/promises';
import { join, extname, normalize } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = fileURLToPath(new URL('../build', import.meta.url));
const PORT = Number(process.env.PORT ?? 5173);
const HOST = process.env.HOST ?? '0.0.0.0';

const TYPES = {
	'.html': 'text/html; charset=utf-8',
	'.js': 'text/javascript; charset=utf-8',
	'.mjs': 'text/javascript; charset=utf-8',
	'.css': 'text/css; charset=utf-8',
	'.json': 'application/json; charset=utf-8',
	'.svg': 'image/svg+xml',
	'.png': 'image/png',
	'.jpg': 'image/jpeg',
	'.ico': 'image/x-icon',
	'.woff': 'font/woff',
	'.woff2': 'font/woff2',
	'.map': 'application/json; charset=utf-8'
};

async function tryFile(path) {
	try {
		const s = await stat(path);
		if (s.isFile()) return path;
	} catch {
		/* not a file */
	}
	return null;
}

const server = createServer(async (req, res) => {
	try {
		// Strip query, prevent path traversal, resolve within ROOT.
		const urlPath = decodeURIComponent((req.url ?? '/').split('?')[0]);
		const rel = normalize(urlPath).replace(/^(\.\.[/\\])+/, '');
		let file = await tryFile(join(ROOT, rel));

		// SPA fallback: no matching asset → serve the app shell.
		if (!file) file = join(ROOT, 'index.html');

		const body = await readFile(file);
		res.writeHead(200, { 'content-type': TYPES[extname(file)] ?? 'application/octet-stream' });
		res.end(body);
	} catch (err) {
		res.writeHead(500);
		res.end(`serve error: ${err}`);
	}
});

server.listen(PORT, HOST, () => {
	console.log(`serving ${ROOT} on http://${HOST}:${PORT}`);
});
