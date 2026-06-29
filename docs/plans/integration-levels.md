# Plan: Workspace vs. user integration levels

## Goal

Today an integration is one row per `(org_id, provider)` — implicitly **workspace-level**.
Add a `level` dimension so each provider can be connected at **workspace** level (org-wide
install/bot, as today) *and* **user** level (per-user OAuth token, to act as the user for
2-way sync). Add the user-level OAuth flows for all three providers and surface both levels
in status + settings UI.

## Decisions (locked)

- **One table.** Add `level` + `connected_user_id`; PK `(org_id, provider, level, connected_user_id)`
  with `connected_user_id` defaulting to `''` for workspace rows.
- **Full scope minus onboarding:** data model + status + user-level OAuth flows.
  Onboarding has already been **purged** (commit `118dcad` on `main`) and will be
  reintroduced later once the base product is done — it is NOT part of this feature.
- **All three providers need distinct user OAuth:**
  - GitHub user-to-server token (`ghu_`), via `login/oauth/authorize` → `login/oauth/access_token`
    (distinct from the App-installation flow, which stays workspace).
  - Slack `user_scope` → `xoxp` token under `authed_user.access_token` (bot `xoxb` stays the
    workspace row). One Slack app, two token types.
  - Linear without `actor=app` → user-scoped token.
- **No provider is required, at any level.** This is generic — NOT a Linear special case.
  Every `(org, provider, level)` row is independent; the backend never blocks on any provider
  being connected; the settings UI renders all three identically (each with its own
  connect/disconnect per level, none marked required, none gating another). The app is fully
  usable with any subset connected (including none). Do NOT add any per-provider
  required/optional logic — the only "required" logic that ever existed was in onboarding
  (`allDone = githubDone && slackDone`), and onboarding has been purged.

## 1. Store / schema

- **New migration `0004_integration_level.sql`:** add `level text NOT NULL DEFAULT 'workspace'`
  and `connected_user_id text NOT NULL DEFAULT ''`; drop old PK, add
  `PRIMARY KEY (org_id, provider, level, connected_user_id)`. Existing rows migrate to
  `workspace`/`''` via defaults — no data loss.
- **`store/integrations.go`:** add `Level` (typed `LevelWorkspace`/`LevelUser`) +
  `ConnectedUserID` to `Integration`; thread through `UpsertIntegration` (new conflict target),
  `GetIntegration`, `DeleteIntegration`; add `ListUserIntegrations(orgID, userID)`.

## 2. Service (`integrations/service.go`)

- `oauthState` gains `Level`.
- `Status(ctx, orgID, userID)` returns, per provider, **both** a workspace status and the
  caller's user status (`ProviderStatus.Level` + `ConnectedByMe`).
- `Disconnect(ctx, orgID, userID, provider, level)` — user level deletes only the caller's row;
  workspace level deletes the org row.

## 3. User-level OAuth flows

Level-aware connect/callback per provider (branch on `?level=user`, default `workspace`; seal
level into state; user callback stores `level='user', connected_user_id=<uid>`):

- **GitHub** (`github_user.go`): authorize `https://github.com/login/oauth/authorize`
  (client_id/redirect_uri/state) → exchange at `/login/oauth/access_token` → store encrypted
  `ghu_` token.
- **Slack** (`slack.go`): add `user_scope` to the authorize URL; on callback read
  `authed_user.access_token` (`xoxp`) and store as the user row.
- **Linear** (`linear.go`): authorize **without** `actor=app` → user token as user row.
- **Routing:** reuse existing `/integrations/{p}/connect` + `/callback` routes via `?level=` —
  no new routes. (GitHub dispatches internally on level since user-token uses a different
  authorize URL than app-install.)
- **Config:** add `user_scopes` for Slack/GitHub where they differ from bot scopes; reuse
  existing client creds.

## 4. Spec + generated code

- `spec/openapi.yaml`: `IntegrationStatus` gains `level` (`workspace|user`) + `connectedByMe`;
  response lists per-provider × per-level; `disconnectIntegration` + `connect` take optional
  `level` query param. (The onboarding wording in the `getIntegrationStatus` description was
  already scrubbed during the onboarding purge.)
- `make generate` regenerates Go (`internal/api/*_gen.go`) and TS (`schema.d.ts`).
- `httpapi/handler.go`: pass `user.ID` into `Status`/`Disconnect`, map new fields.

## 5. Frontend (settings only)

- `lib/integrations.ts`: types gain `level`/`connectedByMe`; `connect(provider, level)`,
  `disconnect(provider, level)`, `statusOf(state, provider, level)`.
- `settings/integrations/+page.svelte`: each provider card shows **two rows** — "Workspace"
  and "Your account" — each with its own badge + connect/disconnect as **icon buttons with
  matching tooltips** (per CLAUDE.md + TODO). All providers rendered identically; none marked
  required; no gating.
- **Onboarding: already purged** (done — see the completed prerequisite below); not part of
  this feature.

## 6. Tests

- Store: workspace + user rows coexist for the same `(org, provider)`; user-disconnect leaves
  the workspace row intact.
- Service: `Status` returns both levels; state seals/opens with `Level`.

## Onboarding purge — DONE (prerequisite, commit `118dcad`)

Already merged to `main`; reintroduced later once the base product is done. Recorded here for
context. What landed:

- Deleted `frontend/src/routes/onboarding/` (the `+page.svelte`).
- `backend/internal/integrations/service.go` `redirectAfter`: OAuth callbacks now redirect to
  `/settings/integrations` (was `/onboarding`); the settings page already parses the
  `?provider=&status=` flags and refetches.
- `frontend/src/routes/+page.svelte`: removed the "Finish setup" → `/onboarding` button (now
  always "Manage integrations") and the unused `integrationsComplete` derivation.
- `frontend/src/lib/components/app/app-shell.svelte`: removed the `/onboarding` breadcrumb.
- README + `spec/openapi.yaml`: scrubbed onboarding wording; Go (ogen) + TS regenerated.

## Files

| File | Change |
|---|---|
| `store/migrations/0004_integration_level.sql` | new |
| `store/integrations.go` | level/user-aware CRUD + `ListUserIntegrations` |
| `integrations/service.go` | level in state; `Status`/`Disconnect` signatures; `redirectAfter` → settings |
| `integrations/github_user.go` | new — GitHub user-to-server flow |
| `integrations/slack.go`, `linear.go` | user flows |
| `config/config.go`, `config.yaml`, `.env.example` | user scopes |
| `spec/openapi.yaml` + regenerated `*_gen.go` / `schema.d.ts` | level fields + params |
| `httpapi/handler.go` | thread userID + map level |
| `frontend/lib/integrations.ts`, `settings/integrations/+page.svelte` | per-level UI |
| `*_test.go` | store + service coverage |

(Onboarding-purge files are not listed here — that prerequisite already landed; see the
"Onboarding purge — DONE" section above.)

## Verification

`cd backend && make generate && go build ./... && go test ./...`;
`cd frontend && npm run generate && npm run build`.
Manual: connect GitHub+Slack at both levels → two coexisting rows; user-disconnect preserves
workspace; app fully usable with any subset (including Linear unconnected at both levels).

## Commits (on `main`; no remote → "merge to main" == commit to main)

0. ~~Purge onboarding.~~ DONE (commit `118dcad`).
1. Data model + status + spec/regen.
2. User OAuth flows + settings UI.
