// Better Stack session replay / RUM — cloud dashboard only.
//
// Gated on PUBLIC_BETTER_STACK_TOKEN at build time: Cloudflare prod/preview
// builds set the token; the self-hosted nginx image builds with an empty
// string so the tag never loads.
import { PUBLIC_BETTER_STACK_TOKEN } from '$env/static/public';

declare global {
	interface Window {
		betterstack?: (...args: unknown[]) => void;
	}
}

const token = (PUBLIC_BETTER_STACK_TOKEN ?? '').trim();

/** True when this build includes a Better Stack JS tag token. */
export const betterStackEnabled = token.length > 0;

let inited = false;

/** Load the JS tag and call init once. No-op when the build has no token. */
export function initBetterStack(environment: string): void {
	if (inited || !token || typeof window === 'undefined') return;
	inited = true;

	// Snippet from https://betterstack.com/docs/errors/js-tag/install/
	const b = window as Window & {
		betterstack: ((...args: unknown[]) => void) & { q?: unknown[]; l?: number };
	};
	b.betterstack =
		b.betterstack ||
		function (...args: unknown[]) {
			(b.betterstack.q = b.betterstack.q || []).push(args);
		};
	b.betterstack.l = +new Date();
	const s = document.createElement('script');
	s.async = true;
	s.crossOrigin = 'anonymous';
	s.src = `https://betterstack.net/b.js?t=${encodeURIComponent(token)}`;
	(document.head || document.getElementsByTagName('head')[0]).appendChild(s);

	b.betterstack('init', { environment });
}

export type BetterStackUser = {
	id: string;
	email: string;
	username: string;
	group_id?: string;
	group_name?: string;
};

/** Associate the current session with a signed-in user (and active org). */
export function identifyBetterStackUser(user: BetterStackUser): void {
	if (!token || typeof window === 'undefined' || !window.betterstack) return;
	window.betterstack('user', user);
}

/** Clear user association on sign-out. */
export function clearBetterStackUser(): void {
	if (!token || typeof window === 'undefined' || !window.betterstack) return;
	window.betterstack('user', null);
}
