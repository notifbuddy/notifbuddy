# Better Stack (session replay + traces)

Cloud-only telemetry. Self-hosted installs leave this off (Helm `otel.enabled: false`;
dashboard image builds with an empty `PUBLIC_BETTER_STACK_TOKEN`).

Axiom remains the log shipper. Better Stack is used for **session replay** (dashboard)
and **OTLP traces** (backend).

## One-time setup in Better Stack

1. **Frontend application** (session replay / RUM)
   - Create an application → Frontend tab → copy the JS tag token.
   - Enable **Record session replays**. Optionally set exclude selectors for
     sensitive UI beyond the default password-field blocking.

2. **Spans source** (OTLP traces)
   - Create a source with platform OpenTelemetry / spans.
   - Copy the **ingesting host** (e.g. `https://….betterstackdata.com`) and
     **source token**.

## Infisical secrets

| Secret | Path | Used by |
|--------|------|---------|
| `BETTER_STACK_OTLP_ENDPOINT` | `/` (prod root) | Backend Cloud Run (`otel.endpoint`) — ingest host with `https://`, no `/v1/traces` path |
| `BETTER_STACK_SOURCE_TOKEN` | `/` (prod root) | Backend Cloud Run (`otel.token`) — Bearer token for OTLP |
| `PUBLIC_BETTER_STACK_TOKEN` | `/frontend` | Dashboard Cloudflare build — JS tag token (browser-visible) |

Terraform already merges Infisical `/` into the backend env. Dashboard deploy
fetches `/frontend` **before** `npm run build` so the token is baked into the
SPA; Cloudflare API tokens still load from `/` after the build.

**Before deploying backend with `otel.enabled: true`**, add
`BETTER_STACK_OTLP_ENDPOINT` and `BETTER_STACK_SOURCE_TOKEN` to Infisical `/`.
Missing values are a hard startup error (same pattern as Axiom).

## Local / self-host verification (always dark)

- `config/backend/local.yaml` and Helm configmap: `otel.enabled: false`
- Dashboard image / local vite: `PUBLIC_BETTER_STACK_TOKEN` empty → no `betterstack.net` requests
- After cloud deploy: Better Stack Live tail (spans) + Frontend verify (replays)