// Better Auth configuration — the single source of truth for users, sessions,
// organizations, memberships, and invitations, in our own Postgres (local pg
// in dev, Neon in prod). The service is fully request-driven: no daemons, no
// cron — safe to scale to zero (NOT-20).
//
// All settings come from config.ts (YAML + ${VAR} env refs); required values
// are validated there at load, so this module can use them unconditionally.
import { betterAuth } from 'better-auth';
import { organization } from 'better-auth/plugins';
import pg from 'pg';
import { config } from './config.ts';
import { sendEmail } from './email.ts';

const pool = new pg.Pool({ connectionString: config.database.url });

export const auth = betterAuth({
	baseURL: config.auth.base_url,
	secret: config.auth.secret,
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

	// GitHub is the only sign-in method — no email/password.
	socialProviders: {
		github: {
			clientId: config.github.client_id,
			clientSecret: config.github.client_secret,
		},
	},

	plugins: [
		organization({
			sendInvitationEmail: async ({ email, inviter, organization: org, invitation }) => {
				const url = `${config.auth.base_url}/accept-invitation/${invitation.id}`;
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
	trustedOrigins: config.cors.trusted_origins,

	advanced: {
		...(config.auth.cookie_domain
			? {
					crossSubDomainCookies: {
						enabled: true,
						domain: config.auth.cookie_domain, // ".notifbuddy.com" in prod: api.* must see the session
					},
				}
			: {}),
	},
});
