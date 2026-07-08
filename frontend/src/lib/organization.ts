import { api } from '$lib/api/client';
import type { components } from '$lib/api/schema';

export type Member = components['schemas']['MemberResponse'];
export type Invitation = components['schemas']['InvitationResponse'];
export type Role = components['schemas']['RoleSlug'];

// The organization roles a member can hold, in rank order.
export const ROLES: Role[] = ['admin', 'member', 'viewer'];

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

// Change a member's role in the active org. Admin-only; returns the updated
// member, or null on failure.
export async function updateMemberRole(membershipId: string, role: Role): Promise<Member | null> {
	const { data, error } = await api.PUT('/members/{membershipId}/role', {
		params: { path: { membershipId } },
		body: { role }
	});
	if (error || !data) return null;
	return data as Member;
}

// Invite an email to the active org, optionally with a role. Returns the
// created invitation, or null on failure.
export async function sendInvitation(
	email: string,
	role?: Role
): Promise<Invitation | null> {
	const { data, error } = await api.POST('/invitations', {
		body: { email, ...(role ? { role } : {}) }
	});
	if (error || !data) return null;
	return data as Invitation;
}

// Revoke a pending invitation so its link can no longer be accepted. Returns
// the invitation in its revoked state, or null on failure.
export async function revokeInvitation(invitationId: string): Promise<Invitation | null> {
	const { data, error } = await api.DELETE('/invitations/{invitationId}', {
		params: { path: { invitationId } }
	});
	if (error || !data) return null;
	return data as Invitation;
}

export type OrgProfile = components['schemas']['OrgProfileResponse'];

// Fetch the active org's profile (name + avatar). Returns null on failure.
export async function fetchOrgProfile(): Promise<OrgProfile | null> {
	const { data, error } = await api.GET('/organization/profile');
	if (error || !data) return null;
	return data as OrgProfile;
}

// Rename the active org. Admin-only. On failure, error carries the reason
// (WorkOS rejections are passed through, e.g. for default test orgs).
export async function updateOrgName(
	name: string
): Promise<{ profile?: OrgProfile; error?: string }> {
	const { data, error } = await api.PUT('/organization/profile', { body: { name } });
	if (error) return { error: error.message ?? "couldn't rename the organization" };
	if (!data) return { error: "couldn't rename the organization" };
	return { profile: data as OrgProfile };
}

// Upload an avatar image (as a data URL). Admin-only; returns the refreshed
// profile, or null.
export async function uploadOrgAvatar(imageDataUrl: string): Promise<OrgProfile | null> {
	const { data, error } = await api.PUT('/organization/avatar', { body: { imageDataUrl } });
	if (error || !data) return null;
	return data as OrgProfile;
}

// Re-roll the generated avatar's seed (also clears any uploaded image).
// Admin-only; returns the refreshed profile, or null.
export async function regenerateOrgAvatar(): Promise<OrgProfile | null> {
	const { data, error } = await api.POST('/organization/avatar/regenerate');
	if (error || !data) return null;
	return data as OrgProfile;
}

// Remove the uploaded avatar so the generated one shows again. Admin-only;
// returns the refreshed profile, or null.
export async function deleteOrgAvatar(): Promise<OrgProfile | null> {
	const { data, error } = await api.DELETE('/organization/avatar');
	if (error || !data) return null;
	return data as OrgProfile;
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
