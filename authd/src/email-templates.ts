// Branded HTML email bodies (src/emails/*.html), read once at module load.
// Mustache HTML-escapes interpolated values — names are user-controlled.
import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import Mustache from 'mustache';

const welcomeTemplate = readFileSync(join(import.meta.dirname, 'emails', 'welcome.html'), 'utf8');
const inviteTemplate = readFileSync(join(import.meta.dirname, 'emails', 'invite.html'), 'utf8');

export function welcomeEmail(vars: { firstName: string; dashboardUrl: string; communitySlackUrl: string }) {
	return {
		subject: `Welcome to notifbuddy, ${vars.firstName}`,
		html: Mustache.render(welcomeTemplate, {
			FirstName: vars.firstName,
			DashboardURL: vars.dashboardUrl,
			CommunitySlackURL: vars.communitySlackUrl,
		}),
		text: `Welcome to notifbuddy, ${vars.firstName}!\n\nnotifbuddy keeps Linear and Slack in sync — both ways. Open your dashboard to get set up: ${vars.dashboardUrl}`,
	};
}

export function inviteEmail(vars: {
	inviterName: string;
	inviterAvatarUrl?: string | null;
	teamName: string;
	inviteUrl: string;
	expiresInHours: number;
}) {
	return {
		subject: `${vars.inviterName} invited you to ${vars.teamName} on notifbuddy`,
		html: Mustache.render(inviteTemplate, {
			InviterName: vars.inviterName,
			// Falsy → Mustache renders the {{^InviterAvatarURL}} initial chip.
			InviterAvatarURL: vars.inviterAvatarUrl || '',
			InviterInitial: (vars.inviterName.trim()[0] || 'n').toUpperCase(),
			TeamName: vars.teamName,
			InviteURL: vars.inviteUrl,
			ExpiresInHours: String(vars.expiresInHours),
		}),
		text: `${vars.inviterName} invited you to join ${vars.teamName} on notifbuddy: ${vars.inviteUrl}\n\nThis invitation expires in ${vars.expiresInHours} hours.`,
	};
}
