import { api, apiBaseUrl } from '$lib/api/client';
import { authClient } from '$lib/auth-client';
import { clearBetterStackUser } from '$lib/betterstack';
import type { components } from '$lib/api/schema';

export type User = components['schemas']['UserResponse'];
export type Organization = components['schemas']['Organization'];

// Shared, reactive auth state. `user` is:
//   undefined → not yet loaded
//   null      → loaded, signed out
//   User      → signed in
// Lives in a `.svelte.ts` module so the rune-backed state is shared across the
// TopBar and every page that imports it — we fetch /me once per page load.
class UserStore {
	user = $state<User | null | undefined>(undefined);
	private inflight: Promise<User | null> | null = null;

	// Fetch /me at most once concurrently; subsequent callers share the result.
	// Pass force to re-fetch (e.g. after a profile change).
	async load(force = false): Promise<User | null> {
		if (!force && this.user !== undefined) return this.user;
		if (!force && this.inflight) return this.inflight;
		this.inflight = (async () => {
			try {
				const { data, error } = await api.GET('/me');
				this.user = error || !data ? null : (data as User);
			} catch {
				// Network/CORS failure: treat as signed out rather than hanging on
				// the "still loading" (undefined) state forever.
				this.user = null;
			}
			this.inflight = null;
			return this.user;
		})();
		return this.inflight;
	}

	// The organization the current session is scoped to, if any.
	get activeOrg(): Organization | undefined {
		const u = this.user;
		if (!u) return undefined;
		return u.organizations?.find((o) => o.id === u.organizationId);
	}
}

export const userStore = new UserStore();

// Sign-in/out run against authd (Better Auth) directly; the backend only
// consumes the resulting session cookie.
export async function signInWithGithub() {
	await authClient.signIn.social({
		provider: 'github',
		callbackURL: `${window.location.origin}/`
	});
}

export async function signOut() {
	clearBetterStackUser();
	await authClient.signOut();
	window.location.href = '/';
}

// Switch the session's active organization, then reload so every surface
// (including the backend-derived /me) sees the new scope. The no-cache /me
// primes the backend past its short session cache (it can't observe the
// setActive, which goes browser → authd directly).
export async function switchOrg(organizationId: string) {
	await authClient.organization.setActive({ organizationId });
	await fetch(`${apiBaseUrl}/me`, {
		credentials: 'include',
		headers: { 'Cache-Control': 'no-cache' }
	});
	window.location.reload();
}

// Human-friendly name for display: full name, falling back to email.
export function displayName(user: User): string {
	const full = [user.firstName, user.lastName].filter(Boolean).join(' ').trim();
	return full || user.email;
}

// The user's profile picture URL (GitHub avatar for GitHub logins), or undefined
// if WorkOS didn't capture one — callers pair it with an initials fallback.
export function avatarUrl(user: User): string | undefined {
	return user.profilePictureUrl || undefined;
}

// Up-to-two-letter initials for the avatar fallback.
export function initials(user: User): string {
	const first = user.firstName?.trim()?.[0];
	const last = user.lastName?.trim()?.[0];
	if (first && last) return (first + last).toUpperCase();
	if (first) return first.toUpperCase();
	return user.email[0]?.toUpperCase() ?? '?';
}
