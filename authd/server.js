// authd — plain Node HTTP server around Better Auth's handler. Everything is
// request-scoped; the process holds no state worth keeping, so Cloud Run can
// scale it to zero freely.
import { createServer } from 'node:http';
import { toNodeHandler } from 'better-auth/node';
import { auth } from './auth.js';

const port = Number(process.env.PORT ?? 8787);
const handler = toNodeHandler(auth);

// CORS for the SPA: Better Auth's trustedOrigins handles CSRF, not CORS —
// browser calls from the dashboard origin still need the headers (with
// credentials, so the origin is echoed, never *).
const trustedOrigins = (process.env.TRUSTED_ORIGINS ?? 'http://localhost:5173').split(',');

function applyCors(req, res) {
	const origin = req.headers.origin;
	if (!origin || !trustedOrigins.includes(origin)) return false;
	res.setHeader('Access-Control-Allow-Origin', origin);
	res.setHeader('Access-Control-Allow-Credentials', 'true');
	res.setHeader('Vary', 'Origin');
	if (req.method === 'OPTIONS') {
		res.setHeader('Access-Control-Allow-Methods', 'GET,POST,OPTIONS');
		res.setHeader(
			'Access-Control-Allow-Headers',
			req.headers['access-control-request-headers'] ?? 'Content-Type',
		);
		res.setHeader('Access-Control-Max-Age', '600');
		res.writeHead(204).end();
		return true; // preflight fully handled
	}
	return false;
}

const server = createServer((req, res) => {
	if (applyCors(req, res)) return;
	if (req.url === '/healthz') {
		res.writeHead(200, { 'content-type': 'text/plain' }).end('ok');
		return;
	}
	handler(req, res);
});

server.listen(port, () => {
	console.log(`authd: listening on :${port}`);
});
