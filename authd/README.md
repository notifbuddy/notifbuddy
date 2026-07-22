# authd

notifbuddy's auth service: [Better Auth](https://better-auth.com) with the
organization plugin — users, sessions, orgs, memberships, and invitations in
our own Postgres (local pg in dev, Neon in prod). One auth platform for cloud
and self-host (NOT-20).

Fully request-driven — no daemons, no cron — so it scales to zero.

## Local dev

```sh
psql -d postgres -c "CREATE DATABASE authd;"
cp .env.example .env   # fill BETTER_AUTH_SECRET (openssl rand -base64 32)
                       # and GITHUB_CLIENT_ID/SECRET (GitHub OAuth app; local/prod)
npm install
npm run migrate        # applies the Better Auth schema
node --env-file=.env src/server.ts
```

The dashboard SPA talks to authd directly (sign-in, org create); the Go
backend validates sessions by calling `GET /api/auth/get-session` with the
browser's cookie.

## Configuration

Non-sensitive settings live under the repo-root config tree:

- `config/authd/${NB_ENV}.yaml` — service settings (`NB_ENV` defaults to `local`)
- `config/featureflags/${NB_ENV}.yaml` — which sign-in methods are enabled

Override paths with `CONFIG_FILE` / `FEATUREFLAGS_FILE`. Sensitive values use
`${VAR}` — resolved at startup; referenced-but-unset is a hard error. `.env`
holds only those secrets (see `.env.example`).

Sign-in methods (from feature flags):

- **local / prod:** `github_oauth_login: true` — GitHub OAuth required
  (`github.client_id` / `client_secret`)
- **preview:** `email_password_login: true` — email/password only (no GitHub
  secrets needed)

Resend email is optional: an empty `email.resend_api_key` sends invitation mail
to the console (dev sink).
