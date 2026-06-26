# Xolo — spec-first ping/pong

A minimal full-stack app demonstrating a **spec-first REST workflow** with
**WorkOS AuthKit** login. A single hand-written OpenAPI document is the source
of truth; both the Go server stub and the TypeScript client are **generated**
from it. There is no hand-written transport code for the JSON API — only
business logic, the auth redirect flow, and UI.

Sign in via WorkOS's hosted AuthKit page; the session is carried in an HttpOnly,
encrypted ("sealed") cookie. `GET /ping` and `GET /me` require that session.

```
spec/openapi.yaml  ──┬──▶  ogen                ──▶  backend/internal/api/  (Go server stub)
  (source of truth)  └──▶  openapi-typescript  ──▶  frontend/src/lib/api/schema.d.ts (TS types)
```

## Stack

| Layer        | Tech                                                           |
| ------------ | ------------------------------------------------------------- |
| Contract     | OpenAPI 3.0.3 (`spec/openapi.yaml`)                          |
| Backend      | Go 1.25, [ogen](https://github.com/ogen-go/ogen) (server gen) |
| Frontend     | SvelteKit (SPA via `adapter-static`), shadcn-svelte, Tailwind v4 |
| API client   | [openapi-typescript](https://openapi-ts.dev) + openapi-fetch  |
| Auth         | [WorkOS AuthKit](https://workos.com/docs/authkit) via [workos-go v9](https://github.com/workos/workos-go) (hosted login, sealed-session cookie) |
| Transport    | Browser → Go directly (credentialed CORS, no proxy)           |

## What is generated vs. hand-written

**Generated (never edit):**

- `backend/internal/api/*.gen.go` — router, request/response types, validation, the `Handler` interface, and a Go client. (ogen)
- `frontend/src/lib/api/schema.d.ts` — TypeScript types for every path and schema. (openapi-typescript)

**Hand-written (the only non-generated code):**

- `backend/handler.go` — implements `api.Handler` (`Ping`, `GetMe`); both require a session.
- `backend/auth.go` — WorkOS client, the `/auth/*` redirect handlers, and the session-loading middleware.
- `backend/config.go` — loads `config.yaml` and resolves `$VAR` secret references.
- `backend/main.go`, `backend/cors.go` — server bootstrap (mux) + credentialed CORS.
- `frontend/src/lib/api/client.ts` — typed `createClient<paths>()` with `credentials: 'include'`.
- `frontend/src/routes/+page.svelte` — the UI (signed-in / signed-out states).

**Deliberate exception to "no hand-written transport":** the three browser
redirect routes — `GET /auth/login`, `GET /auth/callback`, `GET /auth/logout` —
are plain `net/http` handlers, **not** in the spec. They are 302 redirects that
set/clear the session cookie, not JSON operations, which ogen does not model
cleanly. Everything else — `/ping`, `/me`, `POST /auth/verify-email`,
`GET /auth/pending-orgs`, `POST /auth/select-org`, and the `/invitations`
endpoints — stays fully spec-driven.

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

## Configuration

The backend reads a **YAML config file** (`backend/config.yaml`, override with
`CONFIG_FILE`). Non-sensitive values are written literally; **sensitive fields
reference an environment variable** with `$VAR` / `${VAR}`, resolved at startup.
A referenced-but-unset variable is a hard error, so the server never starts with
an empty secret. This keeps real secrets out of the committed file — they live
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
├── spec/openapi.yaml          # source of truth (/ping, /me — JSON only)
├── backend/                   # Go (ogen + WorkOS)
│   ├── handler.go  main.go  cors.go  generate.go
│   ├── auth.go                # WorkOS client + /auth/* redirects + session middleware
│   ├── config.go  config.yaml # YAML config; secrets via $VAR env refs
│   ├── .env.example           # copy to .env and fill in WorkOS secrets
│   └── internal/api/          # GENERATED
├── frontend/                  # SvelteKit SPA
│   └── src/lib/api/           # client.ts + GENERATED schema.d.ts
└── Makefile
```
