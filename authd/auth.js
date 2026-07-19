// Better Auth configuration — the single source of truth for users, sessions,
// organizations, memberships, and invitations, in our own Postgres (local pg
// in dev, Neon in prod). The service is fully request-driven: no daemons, no
// cron — safe to scale to zero (NOT-20).
import { betterAuth } from 'better-auth';
import { organization } from 'better-auth/plugins';
import pg from 'pg';
import { sendEmail } from './email.js';

const {
	DATABASE_URL,
	BETTER_AUTH_URL,
	GITHUB_CLIENT_ID,
	GITHUB_CLIENT_SECRET,
	TRUSTED_ORIGINS,
	COOKIE_DOMAIN,
} = process.env;

if (!DATABASE_URL) throw new Error('authd: DATABASE_URL is required');

const pool = new pg.Pool({ connectionString: DATABASE_URL });

// GitHub is the only sign-in method — no email/password. Missing creds is a
// loud startup error, never a silent auth-less service.
if (!GITHUB_CLIENT_ID || !GITHUB_CLIENT_SECRET) {
	throw new Error('authd: GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET are required');
}

export const auth = betterAuth({
	baseURL: BETTER_AUTH_URL ?? 'http://localhost:8787',
	database: pool,

	// activeOrganizationId is per-session and starts null — without this hook a
	// returning single-org user would face the org picker on every sign-in.
	// Default new sessions to the user's most recent membership.
	databaseHooks: {
		session: {
			create: {
				before: async (session) => {
					const { rows } = await pool.query(
						'SELECT "organizationId" FROM "member" WHERE "userId" = $1 ORDER BY "createdAt" DESC LIMIT 1',
						[session.userId],
					);
					if (rows.length === 0) return { data: session };
					return { data: { ...session, activeOrganizationId: rows[0].organizationId } };
				},
			},
		},
	},

	socialProviders: {
		github: {
			clientId: GITHUB_CLIENT_ID,
			clientSecret: GITHUB_CLIENT_SECRET,
		},
	},

	plugins: [
		organization({
			sendInvitationEmail: async ({ email, inviter, organization: org, invitation }) => {
				const url = `${BETTER_AUTH_URL}/accept-invitation/${invitation.id}`;
				await sendEmail({
					to: email,
					subject: `${inviter.user.name || inviter.user.email} invited you to ${org.name} on notifbuddy`,
					text: `Join ${org.name} on notifbuddy: ${url}`,
				});
			},
		}),
	],

	// Scale-to-zero rule (NOT-20): rate-limit state must live in the database —
	// the in-memory default dies with the instance and never shares across
	// replicas.
	rateLimit: {
		enabled: true,
		storage: 'database',
	},

	// SPA at a sibling origin (dashboard.<zone> / localhost:5173) calls us
	// directly, so its origin must be trusted for CSRF.
	trustedOrigins: (TRUSTED_ORIGINS ?? 'http://localhost:5173').split(','),

	advanced: {
		...(COOKIE_DOMAIN
			? {
					crossSubDomainCookies: {
						enabled: true,
						domain: COOKIE_DOMAIN, // ".notifbuddy.com" in prod: api.* must see the session
					},
				}
			: {}),
	},
});
