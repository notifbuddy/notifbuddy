import { api } from '$lib/api/client';
import type { components } from '$lib/api/schema';

export type Member = components['schemas']['MemberResponse'];
export type Invitation = components['schemas']['InvitationResponse'];

// Fetch the active org's members. Returns null when unauthenticated.
export async function fetchMembers(): Promise<Member[] | null> {
	const { data, error } = await api.GET('/members');
	if (error || !data) return null;
	return (data.members ?? []) as Member[];
}

// Fetch the active org's invitations. Returns null when unauthenticated.
export async function fetchInvitations(): Promise<Invitation[] | null> {
	const { data, error } = await api.GET('/invitations');
	if (error || !data) return null;
	return (data.invitations ?? []) as Invitation[];
}

// Invite an email to the active org, optionally with a role. Returns the
// created invitation, or null on failure.
export async function sendInvitation(
	email: string,
	role?: string
): Promise<Invitation | null> {
	const { data, error } = await api.POST('/invitations', {
		body: { email, ...(role ? { role } : {}) }
	});
	if (error || !data) return null;
	return data as Invitation;
}

// Human-friendly name for a member: full name, falling back to email.
export function memberName(m: Member): string {
	const full = [m.firstName, m.lastName].filter(Boolean).join(' ').trim();
	return full || m.email;
}

// Up-to-two-letter initials for a member's avatar fallback.
export function memberInitials(m: Member): string {
	const first = m.firstName?.trim()?.[0];
	const last = m.lastName?.trim()?.[0];
	if (first && last) return (first + last).toUpperCase();
	if (first) return first.toUpperCase();
	return m.email[0]?.toUpperCase() ?? '?';
}
