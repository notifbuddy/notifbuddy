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

`run.sh` builds four containers with docker compose, runs the suite once, and
exits with the runner's status code (`0` = all green). It tears the stack down
(`down -v`) on exit.

```
postgres  throwaway DB (tmpfs — wiped every run)
fakeapis  TLS-terminating egress proxy + in-process third-party API fakes
backend   the real server, CONFIG_FILE=config.e2e.yaml, HTTPS_PROXY -> fakeapis
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

**Expand it** by adding a `Host` route in `fakeapis/upstreams.go` — no new
certs, DNS, or app changes. `api.linear.app` and `api.github.com` are already
registered as loud `501` scaffolds; fill in their handlers when a flow needs
them.

Today the only intercepted call is the WorkOS organization-membership list that
`/me` fans out to, answered with an empty list.

### Inbound auth (forged sessions)

Separately, a WorkOS **session cookie** is a symmetric-sealed blob wrapping an
**unsigned** JWT whose signature `AuthenticateSession` never verifies (it only
base64-decodes the payload). So the harness forges any session offline by
sealing a hand-built JWT with the same cookie password the server uses
(`sealSession` in `harness_test.go`). No live WorkOS is required.

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
