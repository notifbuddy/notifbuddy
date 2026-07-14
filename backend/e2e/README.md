# Backend e2e tests

Black-box end-to-end tests for the NotifBuddy backend. They talk to a **fully wired
server** (real Postgres, real pub/sub, real HTTP stack) over the network exactly
like the SPA does — no in-process handlers, no mocks of our own code. Every
external SaaS dependency is disabled or stubbed by `config.e2e.yaml`.

## Run

```sh
cd backend/e2e && ./run.sh
```

or from the repo root:

```sh
make test-e2e
```

`run.sh` builds three containers with docker compose, runs the suite once, and
exits with the runner's status code (`0` = all green). It tears the stack down
(`down -v`) on exit.

```
postgres  throwaway DB (tmpfs — wiped every run)
backend   the real server, CONFIG_FILE=config.e2e.yaml
tests     the e2e-tagged Go suite, run once against backend:8080
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

## How auth works without WorkOS

A WorkOS session cookie is a symmetric-sealed blob wrapping an **unsigned** JWT
whose signature `AuthenticateSession` never verifies (it only base64-decodes the
payload). So the harness forges any session offline by sealing a hand-built JWT
with the same cookie password the server uses (`sealSession` in
`harness_test.go`). No live WorkOS is required.

WorkOS network calls that a few endpoints make best-effort (e.g. `/me` listing a
user's orgs) are null-routed at the container (`api.workos.com -> 127.0.0.1`) so
they fail instantly and the suite stays hermetic.

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
