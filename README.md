# Xolo

An app to sync your github PRs and linear tickets to slack. We keep it simple so
you can focus on a single dashboard for communication. We do 2-way syncs for slack
channels to your github PR or your linear tickets, so whatever you comment on github/linear
will be reflected on your slack channel as well.

## Features

#### Keep github and linear slack channel creation manual

You may have a lot of PRs or linear ticket that will create noise if we create the channels automatically, you can
keep this optional and just ask the 'NotifBuddy' app to create the channel. We support natural langauge queries so
on github you can simply do `@NotifBuddy create slack channel` or `@notifbuddy slack this plz`.

Internally we make a cloudflare workers ai call to figure our the exact command that need to be done.
  
#### Workspace integrations

Workspeace level integrations are available for github account connection, linear account connection and slack workspace
connections.

#### User integrations

To do the two way sync, we need oauth logins to act as user across the apps. Every user needs to connect 
github, linear and slack accounts so we can replicate the messages.

#### Linear workspace settings

Only available when linear workspace is connected to notifbuddy. We support the following for linear -

- Create slack channel on linear issue status (enum drop down of status) or keep channel creation manual. on manual
  users have to @notifbuddy create a channel for this.
- Support github template for naming the channels. For test, we need to give sample event data that will be used for
  channel creation so guess work is limited. Test should be possible via real world data as well. This can be use to
  quickly validate changes or create channels for existing tickets when user first onboards. We'll forward the complete
  event that we use for conditional for example, the event will be { event_type: "linear", linear: raw_event }. 
- Configurable conditional on channel creation using similar github template, this must evaluate to a true condition.
  validation is extremely important here to verify the changes so a test against sample events would be pretty nice.
- Auto add bots feature, accept a list of bots to automatically add them on channel creation. Bots like claude, linear,
  etc. can be added by this.

## Stack

| Layer        | Tech                                                           |
| ------------ | ------------------------------------------------------------- |
| Contract     | OpenAPI 3.0.3 (`spec/openapi.yaml`)                          |
| Backend      | Go 1.25, [ogen](https://github.com/ogen-go/ogen) (server gen) |
| Frontend     | SvelteKit (SPA via `adapter-static`), shadcn-svelte, Tailwind v4 |
| API client   | [openapi-typescript](https://openapi-ts.dev) + openapi-fetch  |
| Auth         | [WorkOS AuthKit](https://workos.com/docs/authkit) via [workos-go v9](https://github.com/workos/workos-go) (hosted login, orgs, sealed-session cookie) |
| Integrations | Slack + Linear OAuth, per-organization, stored in Postgres (GitHub parked until phase 2) |
| Persistence  | Postgres via [pgx](https://github.com/jackc/pgx); auto-migrated on startup |
| Transport    | Browser → Go directly (credentialed CORS, no proxy)           |

## What is generated vs. hand-written

**Generated (never edit):**

- `backend/internal/api/*.gen.go` — router, request/response types, validation, the `Handler` interface, and a Go client. (ogen)
- `frontend/src/lib/api/schema.d.ts` — TypeScript types for every path and schema. (openapi-typescript)

**Hand-written (the only non-generated code):** the backend is organized into
`internal/` packages with `cmd/server/main.go` doing the wiring:

- `backend/cmd/server/main.go` — wiring only: load config, build store/crypto/auth/integrations, route the mux, serve.
- `backend/internal/httpapi/` — implements the ogen `api.Handler` (delegates to services) + credentialed CORS.
- `backend/internal/auth/` — WorkOS client, `/auth/*` redirect handlers, session-loading middleware, org/role/invitations.
- `backend/internal/config/` — loads `config.yaml`, resolves `$VAR` secret references (reflection-based, recursive).
- `backend/internal/store/` — pgx pool, embedded migrations, the `org_integrations` repository.
- `backend/internal/crypto/` — `Encryptor` interface (local AES-GCM + KMS seam) for token encryption at rest.
- `backend/internal/integrations/` — Slack + Linear OAuth flows, status, disconnect.
- `frontend/src/lib/api/client.ts`, `src/lib/integrations.ts` — typed client + integration helpers.
- `frontend/src/routes/+page.svelte`, `routes/settings/integrations/` — the UI.

**Deliberate exception to "no hand-written transport":** the browser redirect
routes — `GET /auth/{login,callback,logout}` and
`GET /integrations/{slack,linear}/{connect,callback}` — are plain `net/http`
handlers, **not** in the spec. They are 302 redirects (OAuth/installation flows
that set cookies), not JSON operations, which ogen does not model cleanly.
Everything else — `/ping`, `/me`, `/auth/verify-email`, `/auth/pending-orgs`,
`/auth/select-org`, `/invitations`, and `/integrations/status` +
`/integrations/{provider}/disconnect` — stays fully spec-driven.

## Prerequisites

- Go 1.25+
- Node 20+ (tested on 25)
- A [WorkOS account](https://dashboard.workos.com) with an application (for the API key + client ID).

## WorkOS setup (one time)

1. In the WorkOS dashboard, grab your **API key** (`sk_…`) and **Client ID** (`client_…`).
2. Under **Redirects**, add `http://localhost:8080/auth/callback` as a redirect URI.
3. (Optional) Configure which auth methods AuthKit offers (email/password, social, SSO).
4. Put your secrets in `backend/.env` (the env vars `config.yaml` references):
   ```bash
   cp backend/.env.example backend/.env
   # edit backend/.env — set WORKOS_API_KEY, WORKOS_CLIENT_ID,
   # and a 32+ char WORKOS_COOKIE_PASSWORD (e.g. `openssl rand -base64 32`)
   ```
   The backend auto-loads `backend/.env` at startup (via godotenv), so no shell
   sourcing is needed — `make dev-backend` picks it up.
5. **If you enabled only one social provider** (e.g. GitHub-only): `config.yaml`
   already sets `login_provider: GitHubOAuth`. AuthKit can't render a method
   selector for a single social connection, so this sends users straight to it.
   Set it to `""` if you want the AuthKit selector instead.

## Quick start

```bash
make install      # go mod download + npm install
make generate     # regenerate server stub + TS client from the spec

# In two terminals (backend auto-loads backend/.env — no sourcing needed):
make dev-backend  # Go API on http://localhost:8080
make dev-frontend # SvelteKit on http://localhost:5173
```

Open http://localhost:5173 → click **Sign in with WorkOS** → authenticate on the
AuthKit page → you're redirected back signed in. **Send ping** now returns `pong`;
**Log out** clears the session.

Sanity-check the protected API directly (unauthenticated → 401):

```bash
curl -i http://localhost:8080/ping
# HTTP/1.1 401 Unauthorized
# {"message":"unauthorized"}
```

## Changing the API

1. Edit `spec/openapi.yaml`.
2. Run `make generate`.
3. Implement any new methods the regenerated `api.Handler` interface requires
   (the Go build will tell you which are missing), and use the newly typed
   paths from the frontend client.

That's the whole loop — the contract drives both sides.

## Authentication flow

```
Browser ─ Sign in ─▶ GET /auth/login ─302▶ WorkOS AuthKit (hosted login)
                                              │  user authenticates
        ◀─────────────── 302 + ?code= ────────┘
GET /auth/callback ─▶ AuthenticateWithCode ─▶ seal tokens ─▶ Set-Cookie wos_session
        ─302▶ back to the SPA (now signed in)

SPA load ─▶ GET /me  (cookie ─▶ unseal ─▶ user)   → renders signed-in UI
Send ping ─▶ GET /ping (same cookie)              → 200 pong, or 401 if no session
Log out  ─▶ GET /auth/logout ─▶ clear cookie ─302▶ WorkOS logout ─▶ back to SPA
```

**Email-verification branch** (e.g. first GitHub OAuth login — GitHub users land
unverified, unlike Google/Apple/SSO which WorkOS auto-verifies):

```
GET /auth/callback ─▶ AuthenticateWithCode ─▶ WorkOS: email_verification_required
        ─▶ seal pending_authentication_token in wos_pending cookie
        ─302▶ SPA at /?verify=1   (WorkOS has emailed a code)
SPA ─▶ POST /auth/verify-email {code} ─▶ AuthenticateWithEmailVerification
        ─▶ seal session ─▶ Set-Cookie wos_session, clear wos_pending ─▶ 200 user
```

**Organization-selection branch** (user belongs to more than one organization):

```
GET /auth/callback ─▶ AuthenticateWithCode ─▶ WorkOS: organization_selection_required
        ─▶ seal {pending token, org list} in wos_org_select cookie
        ─302▶ SPA at /?select-org=1
SPA ─▶ GET /auth/pending-orgs        → the orgs to choose from
SPA ─▶ POST /auth/select-org {organizationId}
        ─▶ AuthenticateWithOrganizationSelection ─▶ seal session
        ─▶ Set-Cookie wos_session, clear wos_org_select ─▶ 200 user
```

The session token carries the active `org_id` + `role` (read from the JWT
claims); `GET /me` returns them plus the full list of organizations the user
belongs to.

- The `wos_session` cookie is **HttpOnly + Secure + SameSite=Lax** and holds the
  WorkOS *sealed* session (access + refresh tokens, encrypted with
  `WORKOS_COOKIE_PASSWORD`). JavaScript never reads it. The short-lived
  `wos_pending` cookie (also sealed) carries only the pending token during
  verification and is cleared on success.
- If the access token has expired but the refresh token is still valid, the
  backend refreshes the session and rewrites the cookie transparently on the
  next request — no re-login needed.
- `Secure` cookies are allowed over `http://localhost` by browsers, so this
  works in dev without HTTPS. (Set `app.insecure_cookies: true` only for
  plain-HTTP testing on a non-localhost host.)
- `POST /auth/verify-email` **is** in the spec (JSON in/out, ogen-generated);
  only the three browser-redirect routes (`/auth/login`, `/auth/callback`,
  `/auth/logout`) stay outside it.

## Organizations & invitations

WorkOS provides multi-tenancy (Organizations), memberships, and roles — this app
uses them rather than modelling tenants itself. WorkOS owns identity, org
records, and the membership/role graph; an app would key its own domain data on
the WorkOS `organization_id`.

- **Active org in the session.** After login, the session JWT carries `org_id`
  and `role`. `GET /me` returns `organizationId`, `role`, and `organizations`
  (every org the user belongs to, via `OrganizationMembership.List`). The SPA
  shows the active org + role when signed in.
- **Invitations** (spec-driven JSON endpoints):
  - `POST /invitations {email, role?}` — invite an email to the caller's active
    organization (`SendInvitation`). WorkOS emails the invitee a link.
  - `GET /invitations` — list the active organization's invitations.
  - **Accept-on-login:** an invitee who logs in with an invitation token
    (`/auth/login?invitation_token=…`, round-tripped via AuthKit `state`, or a
    WorkOS invitation link landing on `/auth/callback?invitation_token=…`) has
    the token passed to `AuthenticateWithCode`, which creates their membership.
- **Authorization** is demo-simple: any signed-in member of an org may invite.
  To gate on a role, check `userFromContext(ctx).Role` in `CreateInvitation`.

### Dashboard prerequisites for orgs/invitations

In the WorkOS dashboard (Staging):

1. Create at least one **Organization** and add your user as a member (otherwise
   the session has no `org_id`, and `/invitations` has nothing to target).
2. To assign roles on invite, define the **role** (e.g. `member`) under Roles and
   pass its slug as `role`.

## Integrations (GitHub + Slack)

> **Phase 2 note:** the GitHub integration described below is currently
> **removed from the codebase** — phase 1 focuses on Slack + Linear. The docs
> in this section are kept as the design/setup reference for reintroducing it
> (the removal commit is the restoration guide).

Each WorkOS organization can connect a **GitHub App installation** and a **Slack
workspace**. Integration records (installation id / team id, encrypted tokens,
metadata) live in Postgres keyed by `org_id`; tokens are encrypted at rest by a
pluggable `Encryptor` (local AES-GCM in dev, a customer KMS in prod).

The integrations UI lives on one surface:

- **Integrations settings** (`/settings/integrations`) — persistent management:
  per-provider status + **Connect / Reconnect / Disconnect**. The home card shows
  a summary and links here.

```
Connect  ─▶ GET /integrations/{provider}/connect  (session → sealed state with org+user)
         ─302▶ GitHub App install / Slack OAuth authorize
Callback ─▶ GET /integrations/{provider}/callback (verify state)
   github: store installation_id (tokens minted on demand via App JWT)
   slack:  exchange code → store team_id + ENCRYPTED bot token
         ─302▶ SPA ?provider=&status=
Status   ─▶ GET /integrations/status            → per-provider {connected, account}
Disconnect ▶ POST /integrations/{provider}/disconnect
```

Authorization is demo-simple: any signed-in member of an org may connect. The
GitHub installation is stored, not a long-lived token — short-lived installation
tokens are minted on demand from the App JWT (the GitHub-recommended pattern).

### Database setup

Integrations need Postgres. Either point `DATABASE_URL` at a local server with a
`xolo` database, or use the bundled compose file:

```bash
# Option A — existing local Postgres:
createdb xolo   # then: DATABASE_URL=postgres://localhost:5432/xolo?sslmode=disable

# Option B — Docker:
docker compose up -d postgres
# DATABASE_URL=postgres://xolo:xolo@localhost:5432/xolo?sslmode=disable
```

The backend **auto-migrates** on startup (embedded SQL in `internal/store/migrations`).
If `DATABASE_URL` is unset, the server still runs but integration endpoints report
`configured: false`.

### GitHub webhooks + pub/sub

Incoming GitHub webhooks are handled as **two deliberately separate operations**:

```
POST /integrations/github/webhook
  1. verify X-Hub-Signature-256 HMAC (github.webhook_secret)
  2. STORE the delivery in github_webhook_events  ← source of truth, idempotent on delivery id
  3. PUBLISH integrations.github.webhook_event     ← separate notification, best-effort
  → 202 Accepted   (200 on a redelivery, which is not re-published)
```

Storing and publishing are never merged: the stored row is the durable record
the webhooks view reads; the published event is a notification. If publishing
fails, the handler logs it and still returns 202 — the event is already stored,
so GitHub should not redeliver. A redelivery (same `X-GitHub-Delivery`) is
ack'd without re-storing or re-publishing.

**Pub/sub is provider-agnostic** (`internal/pubsub`): callers depend only on
`pubsub.Publisher` + `pubsub.Message` — no AWS or channel types leak. Backends:

- `memory` (local dev) — in-process bus with subscriber fan-out; a logging
  subscriber prints each published event so the path is visible.
- `sns` (production) — `pubsub.NewSNSPublisher` behind an `SNSClient` seam (like
  the crypto KMS seam); wire your AWS SNS client in `buildPublisher`. Until then
  it falls back to a no-op publisher with a warning.

Select the backend with `pubsub.provider` in `config.yaml`. The
**`/settings/integrations/webhooks`** view lists an org's recent deliveries
(event type, action, time) with expandable payloads, served by the spec-driven
`GET /integrations/github/webhooks`.

## Bidirectional sync (Slack ↔ Linear)

Beyond storing webhooks, xolo runs a **sync engine** (`internal/sync`) that keeps
a Slack channel and a Linear issue in step — one channel per issue, comments
mirrored both ways (including threaded replies). It is event-driven: it
*subscribes* to the ingestion topics and *publishes* a processing topic per
action it takes.

```
Linear webhook ─▶ HandleLinearWebhook ─ store ─ publish integrations.linear.webhook_event
                                                          │
                                                          ▼ Engine.OnLinearEvent
   Defense 1: drop if the event was authored by our app (data.botActor present)
   ├─ Issue reached the trigger status ─▶ create channel (name template + condition),
   │     auto-add bots ─▶ fire sync.slack.channel.created / sync.slack.bots.added
   └─ Comment created ─▶ post into the issue's channel AS the bot, showing the
         author's name+avatar (chat:write.customize) ─▶ fire sync.slack.message.posted

Slack event ─▶ HandleSlackWebhook ─ verify signature ─ store ─ publish integrations.slack.webhook_event
                                                          │
                                                          ▼ Engine.OnSlackEvent
   Defense 1: drop the bot's own messages (bot_id / subtype set)
   └─ Message in a synced channel ─▶ commentCreate on the issue with actor=app +
         createAsUser + displayIconUrl (author's name+avatar) ─▶ fire sync.linear.comment.posted
```

**Attribution (why our writes are safe to ignore).** Every mirrored message is
authored by our **bot/app**, but *displays* the real person:

- Slack: `chat.postMessage` as the bot with `username` + `icon_url` overrides
  (the `chat:write.customize` scope). The message shows the person's name/avatar
  with a small **APP** tag.
- Linear: `commentCreate` with `actor=app` plus `createAsUser` + `displayIconUrl`
  (only available to `actor=app` OAuth tokens). The comment renders as
  *"Name (via NotifBuddy)"*.

**Loop prevention is a single rule (Defense 1).** Because our writes are always
bot/app-authored, their echo arrives tagged as such — a Linear webhook carries a
`data.botActor`, a Slack event carries a `bot_id`/subtype — and the engine drops
it before mirroring it back. That one check breaks the Linear→Slack→Linear cycle
in both directions; there is deliberately no separate "message links" anti-loop
table.

**Routing map (not a loop defense).** Two small tables place messages correctly:
`issue_channels` (the one channel per issue) and `mirrored_messages` (each
mirrored comment ↔ its counterpart, with the thread root's counterpart so a
reply on one side lands under the right parent on the other). These are read to
route and to thread; they are not used to prevent loops.

**Processing topics (fire an event per action).** Every action publishes a
best-effort notification after it succeeds — `sync.slack.channel.created`,
`sync.slack.channel.closed`, `sync.slack.channel.deleted`,
`sync.slack.bots.added`, `sync.slack.message.posted`,
`sync.linear.comment.posted`. In dev the memory bus logs them; in production they
feed the same SNS seam as the ingestion topics (and, later, user-defined
webhooks). Publishing failures are logged, never fatal — the action already
happened.

**Channel creation is settings-driven.** The engine reuses the org's Linear
settings (`internal/store/linear_settings.go`): `creation_mode` (`status` vs
`manual`), the trigger status, the name template and creation condition (both
GitHub-Actions expressions evaluated against the forwarded event via
`internal/template`), and the auto-add-bots list. In `manual` mode a channel is
created only when someone comments **@notifbuddy create channel** — the comment
body is classified by `internal/intent` (Cloudflare Workers AI) into
create/close.

The engine only runs on the in-memory pubsub backend (it subscribes in-process);
the SNS path is consumed by a separate worker, which is not built here.

### Dashboard prerequisites for integrations

- **GitHub App** ([create one](https://github.com/settings/apps/new)): set the
  callback + setup URL to `http://localhost:8080/integrations/github/callback`,
  set the **webhook URL** to `http://localhost:8080/integrations/github/webhook`
  with a webhook **secret** (→ `GITHUB_WEBHOOK_SECRET`), generate a private key,
  and note the App ID, slug, and OAuth client id/secret. Put `app_slug`/`app_id`
  in `config.yaml` and the secrets in `.env`. (For local webhook delivery,
  expose `:8080` with a tunnel like ngrok and use that URL.)
- **Slack app**: the fastest path is **Create from manifest**. Go to
  [api.slack.com/apps](https://api.slack.com/apps) → **Create New App** → **From a
  manifest** → pick your workspace → paste the YAML below → Create. Then on the
  app's **Basic Information** page copy the **Client ID** and **Client Secret**
  into `.env` (`SLACK_CLIENT_ID`, `SLACK_CLIENT_SECRET`). The manifest already
  sets the OAuth redirect URL and the bot scopes (matching `slack.scopes` in
  `config.yaml` — trim there and in the manifest to what you actually need).

```yaml
_metadata:
  major_version: 2
  minor_version: 1
display_information:
  name: NotifBuddy
  description: Integrations demo — connects a Slack workspace to your org.
features:
  bot_user:
    display_name: notifbuddy
    always_online: false
oauth_config:
  redirect_urls:
    - https://localhost:8080/integrations/slack/callback
  scopes:
    bot:
      - app_mentions:read
      - channels:manage
      - channels:history
      - channels:read
      - commands
      - groups:history
      - groups:write
      - reactions:write
      - chat:write
      - chat:write.public
      - chat:write.customize   # post as the bot but show a per-message name/avatar (attribution)
      - im:history
      - im:read
      - im:write
      - users:read
      - users:read.email        # resolve a comment author's Slack identity for attribution
      - reactions:read
      - emoji:read
settings:
  event_subscriptions:
    request_url: https://localhost:8080/integrations/slack/webhook
    bot_events:
      - message.channels        # inbound channel messages drive the Slack → Linear sync
  org_deploy_enabled: false
  socket_mode_enabled: false
  token_rotation_enabled: false
```

  Note: Slack requires **https** redirect URLs even for localhost, so the
  manifest uses `https://localhost:8080/...`. For the OAuth callback to actually
  resolve in local dev, either run the backend behind a tunnel (ngrok) and use
  that https URL here and in `slack.callback_url`, or terminate TLS in front of
  `:8080`. Plain `http://localhost` works for GitHub but **not** Slack.

  For the **Slack → Linear** sync direction, enable **Event Subscriptions** on
  the app (Request URL `…/integrations/slack/webhook`, bot event
  `message.channels`) and copy the app's **Signing Secret** (Basic Information)
  into `.env` as `SLACK_SIGNING_SECRET`. Inbound events are verified against it
  (`X-Slack-Signature`, with a ±5-minute timestamp check); an empty secret
  disables verification and thus the Slack → Linear direction. The manifest above
  already declares the event subscription. The `chat:write.customize` and
  `users:read.email` scopes power native message attribution (see
  [Bidirectional sync](#bidirectional-sync-slack--linear)).
- **Linear OAuth app**: in Linear go to **Settings → API → OAuth applications →
  Create**. Set the **redirect URL** to
  `http://localhost:8080/integrations/linear/callback` (must match
  `linear.callback_url`), and copy the **Client ID** / **Client Secret** into
  `.env` (`LINEAR_CLIENT_ID`, `LINEAR_CLIENT_SECRET`). The connect flow requests
  `read,write` scopes and passes `actor=app`, so connecting installs the
  integration into the workspace as an app (a workspace admin approves it);
  actions are attributed to the app rather than the connecting user.
  To receive events, enable **webhooks** on the OAuth app (or create a webhook),
  point them at `POST /integrations/linear/webhook`, and put the signing secret
  in `.env` (`LINEAR_WEBHOOK_SECRET`). Deliveries are HMAC-verified
  (`Linear-Signature`), stored, and viewable per org. As with Slack/GitHub
  webhooks in local dev, expose `:8080` with a tunnel so Linear can reach it.

## Configuration

The backend reads a **YAML config file** (`backend/config.yaml`, override with
`CONFIG_FILE`). Non-sensitive values are written literally; **sensitive fields
reference an environment variable** with `$VAR` / `${VAR}`, resolved at startup
(recursively, over every string field). Unset references expand to empty —
optional integration fields are routinely left unconfigured — and the genuinely
required values (the WorkOS essentials) are enforced afterward by validation with
a precise message. This keeps real secrets out of the committed file — they live
in `backend/.env` (auto-loaded) or the real environment.

```yaml
# backend/config.yaml
server: { addr: ":8080" }
cors: { allow_origin: "http://localhost:5173" }
workos:
  client_id: "${WORKOS_CLIENT_ID}"
  api_key: "${WORKOS_API_KEY}"          # secret — from env
  cookie_password: "${WORKOS_COOKIE_PASSWORD}"  # secret — from env
  redirect_uri: "http://localhost:8080/auth/callback"
  login_provider: "GitHubOAuth"         # "" to show the AuthKit selector
app:
  post_login_url: "http://localhost:5173"
  insecure_cookies: false
```

| Config field (`config.yaml`)   | Env var referenced (in `.env`) | Sensitive | Purpose                                              |
| ------------------------------ | ------------------------------ | --------- | ---------------------------------------------------- |
| `server.addr`                  | —                              |           | Listen address                                       |
| `cors.allow_origin`            | —                              |           | Exact origin allowed to call the API (credentialed CORS) |
| `workos.client_id`             | `WORKOS_CLIENT_ID`             |           | WorkOS application client ID (`client_…`)            |
| `workos.api_key`               | `WORKOS_API_KEY`               | ✅        | WorkOS API key (`sk_…`)                              |
| `workos.cookie_password`       | `WORKOS_COOKIE_PASSWORD`       | ✅        | Seals the session cookie (≥32 chars)                 |
| `workos.redirect_uri`          | —                              |           | OAuth callback (must match a dashboard redirect)     |
| `workos.login_provider`        | —                              |           | Skip the AuthKit selector → one provider (e.g. `GitHubOAuth`) |
| `app.post_login_url`           | —                              |           | Where the browser lands after login/logout (SPA origin) |
| `app.insecure_cookies`         | —                              |           | Drop the cookie `Secure` flag (plain-HTTP testing)   |
| `database.url`                 | `DATABASE_URL`                 | ✅        | Postgres connection string (empty disables integrations) |
| `encryption.provider`          | —                              |           | `local` (AES-GCM) or `kms`                           |
| `encryption.local_key`         | `INTEGRATION_ENC_KEY`          | ✅        | base64 32-byte key for `local` (empty = ephemeral dev key) |
| `pubsub.provider`              | —                              |           | `memory` (local) or `sns` (wire a client in buildPublisher) |
| `pubsub.sns_topic_arn`         | —                              |           | SNS topic ARN when `provider=sns`                    |
| `github.app_slug` / `app_id`   | —                              |           | GitHub App slug + numeric ID                         |
| `github.client_id`             | `GITHUB_APP_CLIENT_ID`         |           | GitHub App OAuth client id                           |
| `github.client_secret`         | `GITHUB_APP_CLIENT_SECRET`     | ✅        | GitHub App OAuth client secret                       |
| `github.private_key`           | `GITHUB_APP_PRIVATE_KEY`       | ✅        | App PEM key (mints installation tokens)              |
| `github.webhook_secret`        | `GITHUB_WEBHOOK_SECRET`        | ✅        | Verifies webhook HMAC (empty disables verification)  |
| `slack.client_id`              | `SLACK_CLIENT_ID`              |           | Slack app client id                                  |
| `slack.client_secret`          | `SLACK_CLIENT_SECRET`          | ✅        | Slack app client secret                              |
| `slack.scopes`                 | —                              |           | Requested bot scopes (space/comma separated)         |
| `slack.signing_secret`         | `SLACK_SIGNING_SECRET`         | ✅        | Verifies inbound Slack Events API requests (empty disables the Slack → Linear sync) |
| `linear.client_id`             | `LINEAR_CLIENT_ID`             |           | Linear OAuth app client id                           |
| `linear.client_secret`         | `LINEAR_CLIENT_SECRET`         | ✅        | Linear OAuth app client secret                       |
| `linear.scopes`                | —                              |           | Requested OAuth scopes (default: read, write)        |
| `linear.webhook_secret`        | `LINEAR_WEBHOOK_SECRET`        | ✅        | Verifies webhook HMAC (empty disables verification)  |

Any field can pull from the environment by setting it to a `$VAR` reference; the
secret fields above do so by default. The frontend reads one env var:
`PUBLIC_API_BASE_URL` (default `http://localhost:8080`) — the base URL the
browser client calls.

## Notes

- ogen prints `Convenient errors are not available ... operation has no "default" response` during generation. That's informational — the spec intentionally has no `default` error response for this demo.
- The frontend is a pure SPA (`ssr = false`, `prerender = true` in `+layout.ts`); the Go server is the only backend at runtime.
- TypeScript is pinned to v5 because `openapi-typescript@7` declares a peer range of `^5.x` (the SvelteKit template otherwise pulls TS 6).
```
xolo/
├── spec/openapi.yaml             # source of truth (JSON endpoints only)
├── docker-compose.yml            # optional local Postgres
├── backend/                      # Go (ogen + WorkOS + integrations)
│   ├── config.yaml  .env.example # YAML config; secrets via $VAR env refs
│   ├── generate.go               # //go:generate ogen directive
│   ├── cmd/server/main.go        # wiring only
│   └── internal/
│       ├── api/                  # GENERATED (ogen)
│       ├── httpapi/              # implements api.Handler + CORS
│       ├── auth/                 # WorkOS: redirects, session, orgs, invitations
│       ├── config/              # YAML + $VAR env-ref loader
│       ├── store/                # pgx pool + migrations + repository
│       ├── crypto/               # Encryptor (local AES-GCM + KMS seam)
│       ├── pubsub/               # provider-agnostic Publisher (memory + SNS seam)
│       └── integrations/         # GitHub App + Slack OAuth + webhook receiver
├── frontend/                     # SvelteKit SPA
│   └── src/
│       ├── lib/                  # api/client.ts (+ GENERATED schema.d.ts), integrations.ts
│       └── routes/               # +page.svelte, settings/integrations/
└── Makefile
```
