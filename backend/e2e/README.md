# e2e tests

Black-box end-to-end tests for NotifBuddy. They talk to a **fully wired server**
(real Postgres, real pub/sub, real HTTP stack) over the network exactly like the
SPA does — no in-process handlers, no mocks of our own code. Every external SaaS
dependency is disabled or stubbed by `config.e2e.yaml`.

Two suites share one docker-compose stack, split by compose **profile**:

- **backend** (`./run.sh`) — the e2e-tagged **Go** suite that drives the API directly.
- **ui** (`./run-ui.sh`) — the SvelteKit **dashboard** driven in a real browser
  (Playwright) against the same backend.

## Run

```sh
cd backend/e2e && ./run.sh       # backend Go suite
cd backend/e2e && ./run-ui.sh    # dashboard Playwright suite
```

or from the repo root:

```sh
make test-e2e        # backend Go suite
make test-e2e-ui     # dashboard Playwright suite
```

Each script builds the stack with docker compose, runs its suite once, and exits
with the runner's status code (`0` = all green). It tears the stack down
(`down -v`) on exit.

```
postgres  throwaway DB (tmpfs — wiped every run)
fakeapis  TLS-terminating egress proxy + third-party API fakes; also mints the
          forged session cookie the UI suite authenticates with
backend   the real server, CONFIG_FILE=config.e2e.yaml, HTTPS_PROXY -> fakeapis
tests     (profile: backend) the e2e-tagged Go suite, against backend:8080
ui        (profile: ui) the dashboard Playwright suite, against localhost:8080
```

## Against an already-running server

The tests are just Go tests behind the `e2e` build tag. Point them at any live
backend:

```sh
E2E_BASE_URL=http://localhost:8080 \
WORKOS_COOKIE_PASSWORD=<the server's cookie password> \
LINEAR_WEBHOOK_SECRET=<the server's linear webhook secret> \
go test -tags e2e -count=1 -v ./e2e/...
```

With `E2E_BASE_URL` unset the package skips itself, so a normal
`go test ./...` never runs it.

## Intercepting third-party APIs (`fakeapis`)

The backend keeps calling the **real** third-party SDKs against their real
hostnames — nothing test-specific leaks into production code. Interception
happens at the network:

- `fakeapis/` is a single Go program: a TLS-terminating forward proxy plus
  in-process fakes. On start it mints a throwaway CA and writes its cert to a
  shared volume.
- The backend gets `HTTPS_PROXY=http://fakeapis:8888` and `SSL_CERT_FILE=<the
  CA>`. Go's default SDK transport (`ProxyFromEnvironment`) tunnels every
  outbound HTTPS call through the proxy, which MITMs the `CONNECT` with a leaf
  cert minted on the fly and dispatches by `Host` to a fake.
- Every captured request is logged (`fakeapis: capture GET api.workos.com/...`).

**Expand it** by registering a `Host` in `fakeapis/dispatch.go` and adding a
per-host package (`fakeapis/workos`, `fakeapis/linear`, `fakeapis/github`) — no
new certs, DNS, or app changes. `api.linear.app` and `api.github.com` are already
registered as loud `501` scaffolds; fill in their handlers when a flow needs
them.

The WorkOS fake answers the calls `/me` and the settings pages fan out to: the
organization-membership list, `GET /organizations/{id}`, and `GET
/user_management/users/{id}` — all describing one shared e2e org/user (see
`fakeapis/session`), so `/me` resolves a complete, org-scoped identity.

### Inbound auth (forged sessions)

Separately, a WorkOS **session cookie** is a symmetric-sealed blob wrapping an
**unsigned** JWT whose signature `AuthenticateSession` never verifies (it only
base64-decodes the payload). So we forge any session offline by sealing a
hand-built JWT with the same cookie password the server uses. The **Go** suite
does this per-test (`sealSession` in `harness_test.go`); the **UI** suite reuses
one identity — `fakeapis` seals it on start into `session.json` on the shared
volume, and the browser is seeded with that cookie (`frontend/e2e/fixtures.ts`).
No live WorkOS is required.

### Dashboard suite (Playwright)

The `ui` container builds the SPA with `PUBLIC_API_BASE_URL=http://localhost:8080`
and serves it on `:5173`. It runs with `network_mode: service:backend`, so from
the browser's view the SPA is `localhost:5173` and the API is `localhost:8080` —
exactly the origins `config.e2e.yaml` already allows (`cors.allow_origin` /
`app.post_login_url`). That makes the SPA→API call cross-origin (CORS is
exercised) but same-site (host `localhost`), so the forged `wos_session` cookie
is sent on every credentialed fetch. Specs live in `frontend/e2e/`.

## Coverage

| Area | Tests |
|------|-------|
| Session / auth gating | `/ping`, `/me` — 401 anonymous, 200 authed, tampered-cookie rejected |
| Identity | `/me` echoes session id/email/org/role |
| CORS | credentialed preflight echoes the exact origin, never `*` |
| Routing | unknown path 404s (no 500) |
| Integration status | fresh org: service configured, nothing connected; 401 anonymous; tenant isolation |
| Linear settings | full create → read → update → delete lifecycle against real Postgres + cross-tenant isolation |
| Template engine | settings-test renders a sample event's identifier; bad event JSON → 400 |
| Linear webhook | HMAC verification — bad/missing signature 401, valid 202, typeless 400 |
| Slack OAuth connect | authed+org-scoped starts the flow with a sealed state; org-less/anonymous cannot |

### Dashboard (Playwright, `frontend/e2e/`)

| Area | Tests |
|------|-------|
| Auth entry | signed-in cookie redirects into `/dashboard/linear` and shows the active org; signed-out shows the login card; deep-links bounce to login |
| Dashboard | Linear tab renders the connect-first empty state; top-nav → Integrations |
| Org switcher | dropdown lists the active organization from `/me` |
| Profile menu | routes to `/settings/billing` |
| Integrations | workspace page lists Slack/Linear providers + Workspace/User tabs; not connected in a fresh org |
| Billing | billing page renders plan options; reachable from the profile menu |

## Notes

- This suite runs against whatever code is checked out. On `main` (all merged
  security fixes) you can additionally assert fix-specific behavior — e.g. the
  parser depth bound (deeply-nested template returns an inline error instead of
  crashing the process), fail-**closed** webhooks when no secret is set, and the
  invite-privilege gate (a member cannot invite an admin → 403). This branch
  predates those fixes, so those assertions are intentionally omitted here.
- Adding a test: drop a `*_test.go` file with `//go:build e2e` in this directory
  and use the `newSession` / `getJSON` / `postJSON` helpers from
  `harness_test.go`.
