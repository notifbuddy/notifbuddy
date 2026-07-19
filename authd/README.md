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
npm install
npm run migrate        # applies the Better Auth schema
node --env-file=.env server.js
```

The dashboard SPA talks to authd directly (sign-in, org create); the Go
backend validates sessions by calling `GET /api/auth/get-session` with the
browser's cookie.

## Configuration

See `.env.example`. GitHub login and Resend email are optional pairs — set
both variables or neither; half a pair fails loudly at boot.
