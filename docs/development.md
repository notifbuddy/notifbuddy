# Local development

This guide covers running the full notifbuddy stack on your machine and
driving a real Linear ⇄ Slack sync against the dev apps. It reflects the
setup used on the primary dev machine; adjust hostnames and paths for yours.

## Architecture at a glance

Four processes cooperate in local dev:

- **backend** (`backend/`, Go, `:8080`) — the API server and sync engine.
  Receives Linear and Slack webhooks, creates and updates Slack channels,
  mirrors messages.
- **authd** (`authd/`, Node, `:8787`) — Better Auth service. Only needed for
  dashboard sign-in; webhook-driven sync works without it.
- **frontend** (`frontend/`, SvelteKit, `:5173`) — the dashboard SPA. Only
  needed to click through settings; the same config is editable via the CLI
  or SQL.
- **cloudflared tunnel** — exposes `:8080` on a stable public hostname so
  Linear and Slack can deliver webhooks to your machine.

## Prerequisites

- Go, Node 20+, and `cloudflared` installed.
- Postgres running locally with `xolo` and `authd` databases. Homebrew
  Postgres works; `docker compose up -d postgres` is the fallback (they both
  want `:5432` — run one, not both).
- `backend/.env` filled in from `backend/.env.example`. The backend auto-loads
  it at startup (godotenv), so `make dev-backend` needs no shell setup.
- `authd/.env` filled in from `authd/.env.example` (see `authd/README.md`).

## The dev tunnel

Webhooks need an internet-reachable URL. The dev setup uses a named
Cloudflare tunnel mapping `local-api.notifbuddy.com` to `localhost:8080`,
configured in `~/.cloudflared/config.yml`:

```yaml
tunnel: notifbuddy-local
credentials-file: ~/.cloudflared/<tunnel-id>.json
ingress:
  - hostname: local-api.notifbuddy.com
    service: http://localhost:8080
  - service: http_status:404
```

Run it with:

```sh
cloudflared tunnel run notifbuddy-local
```

Set `PUBLIC_BASE_URL=https://local-api.notifbuddy.com` in `backend/.env` so
signed asset-proxy links (inline image mirroring) resolve publicly.

## Dev apps (Linear and Slack)

Local dev uses dedicated dev apps, never the production ones:

- **notifbuddy-local** Linear OAuth app — client id inlined in
  `config/backend/local.yaml`; manifest in `backend/manifests/linear.local.json`.
  Its webhook URL must point at
  `https://local-api.notifbuddy.com/integrations/linear/webhook`.
- **notifbuddy-local** Slack app — manifest in
  `backend/manifests/slack.local.yaml`. Its Events API request URL must point
  at `https://local-api.notifbuddy.com/integrations/slack/webhook`.

The secrets for both (webhook secret, client secrets, signing secret) live in
`backend/.env`.

> **Gotcha:** Slack disables an app's Events API subscription after the
> request URL fails repeatedly — which happens every time the tunnel is down
> while someone posts in a synced channel. If Slack → Linear mirroring stops
> working locally, check the app's Event Subscriptions page at
> api.slack.com/apps and re-enable/re-verify the URL. Linear does not do
> this; its deliveries just fail and can be inspected per-webhook in the
> Linear app settings.

## Running the stack

```sh
cloudflared tunnel run notifbuddy-local   # terminal 1
make dev-backend                          # terminal 2 (backend/.env auto-loads)
cd authd && npm start                     # terminal 3 (only for dashboard login)
make dev-frontend                         # terminal 4 (only for the dashboard UI)
```

The backend migrates the database on boot. Health check:

```sh
curl https://local-api.notifbuddy.com/healthz
```

## Driving a sync by hand

The org, integration connections (Slack + Linear at workspace and user
level), and per-team channel rules persist in the local `xolo` database, so
after the first onboarding you rarely touch them again. Useful inspection
queries:

```sql
select org_id, provider, level from org_integrations;
select team_id, creation_mode, trigger_status, name_template, topic_template
from linear_settings;
select linear_issue_id, slack_channel_id, topic from issue_channels;
```

To exercise the auto-create path without clicking through the dashboard,
flip the team config to status mode:

```sql
update linear_settings set creation_mode='status', trigger_status='In Progress'
where team_id='<linear-team-uuid>';
```

Then move any issue in that team to **In Progress** in Linear. Watch the
backend logs: a `pubsub message` on `integrations.linear.webhook_event`
followed by `sync.slack.channel.created` means the whole path worked. The
channel gets its topic backlink and ticket-body intro message on creation,
and the topic re-renders on later issue updates.

Set `creation_mode` back to `manual` when you're done, or every issue you
drag to In Progress will open a channel.

## Code generation

API changes start in `spec/openapi.yaml`, then regenerate all clients:

```sh
make generate   # ogen server (backend), ogen client (cli), TS types (frontend)
```

## Tests

```sh
cd backend && go test ./internal/...   # unit tests
make test-e2e                          # black-box suite in docker compose
```

The e2e suite (`backend/e2e`) runs the real server against fakes for Linear,
Slack-adjacent auth, and sessions; see `config/backend/e2e.yaml` for how the
seams are wired.
