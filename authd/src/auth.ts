// Better Auth configuration — the single source of truth for users, sessions,
// organizations, memberships, and invitations, in our own Postgres (local pg
// in dev, Neon in prod). The service is fully request-driven: no daemons, no
// cron — safe to scale to zero (NOT-20).
//
// Sign-in is Slack OAuth (OIDC) only. Preview uses oAuthProxy so Slack only
// ever callbacks to production auth.notifbuddy.com.
import { betterAuth } from 'better-auth';
import { bearer, deviceAuthorization, oAuthProxy, organization } from 'better-auth/plugins';
import pg from 'pg';
import { config } from './config.ts';
import { sendEmail } from './email.ts';
import { inviteEmail, welcomeEmail } from './email-templates.ts';

const pool = new pg.Pool({ connectionString: config.database.url });

const plugins: Parameters<typeof betterAuth>[0]['plugins'] = [
	bearer(),
	deviceAuthorization({
		verificationUri: config.device_auth?.verification_url || '/device',
		expiresIn: '15m',
		interval: '5s',
		validateClient: async (clientId) => clientId === 'notifbuddy-cli',
	}),
	organization({
		sendInvitationEmail: async ({ email, inviter, organization: org, invitation }) => {
			const url = `${config.auth.base_url}/accept-invitation/${invitation.id}`;
			const expiresInHours = Math.max(
				1,
				Math.round((new Date(invitation.expiresAt).getTime() - Date.now()) / 3_600_000),
			);
			await sendEmail({
				to: email,
				...inviteEmail({
					inviterName: inviter.user.name || inviter.user.email,
					inviterAvatarUrl: inviter.user.image,
					teamName: org.name,
					inviteUrl: url,
					expiresInHours,
				}),
			});
		},
	}),
];

if (config.oauth_proxy?.secret) {
	plugins!.push(
		oAuthProxy({
			productionURL: config.oauth_proxy.production_url,
			secret: config.oauth_proxy.secret,
			// Cloud Run / CF proxy: prefer the configured public base URL over
			// request Host detection so the proxy round-trip finds auth-pr-N.
			currentURL: config.auth.base_url,
		}),
	);
}

export const auth = betterAuth({
	baseURL: config.auth.base_url,
	secret: config.auth.secret,
	database: pool,

	// activeOrganizationId is per-session and starts null — without this hook a
	// returning single-org user would face the org picker on every sign-in.
	// Default new sessions to the user's most recent membership.
	databaseHooks: {
		// First sign-in creates the user row — greet them. Errors only log; a
		// mail outage must never fail signup.
		user: {
			create: {
				after: async (user) => {
					try {
						const firstName = user.name?.trim().split(/\s+/)[0] || user.email.split('@')[0];
						await sendEmail({
							to: user.email,
							...welcomeEmail({
								firstName,
								dashboardUrl: config.email.dashboard_url,
								communitySlackUrl: config.email.community_slack_url,
							}),
						});
					} catch (err) {
						console.error(`authd: welcome email failed for ${user.email}:`, err);
					}
				},
			},
		},
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
		slack: {
			clientId: config.slack.client_id,
			clientSecret: config.slack.client_secret,
		},
	},

	plugins,

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
		...(config.auth.cookie_prefix ? { cookiePrefix: config.auth.cookie_prefix } : {}),
		...(config.auth.cookie_domain
			? {
					crossSubDomainCookies: {
						enabled: true,
						domain: config.auth.cookie_domain,
					},
				}
			: {}),
	},
});
