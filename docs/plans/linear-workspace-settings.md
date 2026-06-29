# Plan: Linear workspace settings + template service

## Goal

Add **Linear workspace settings** (only when Linear is connected at the workspace level)
covering four things from the readme:

1. **Channel-creation trigger** — create a Slack channel automatically when a Linear issue
   reaches a chosen status (enum dropdown), OR keep it manual (`@notifbuddy create a channel`).
2. **Channel name template** — a "GitHub template" (GitHub Actions expression syntax) rendered
   against the event to produce the channel name.
3. **Channel-creation conditional** — a GHA-expression that must evaluate to `true` for a
   channel to be created. Validation is critical → testable against sample events.
4. **Auto-add bots** — a list of bots (claude, linear, …) to add on channel creation.

The naming + conditional both run against a forwarded event envelope:
`{ event_type: "linear", linear: <raw_event> }` (and `{ event_type: "github", github: <raw> }`
for the GitHub reuse).

## Key architectural decision: a standalone `template` service

Like `internal/intent`, the template engine is its own package (`internal/template`) because
the **GitHub** channel-naming/conditional feature will reuse it. It is provider-agnostic: it
operates on the event envelope, not on Linear specifics. "Test with rigour / zero unknowns"
applies here — it is exhaustively tested against real captured GitHub **and** Linear payloads.

### Template language: GitHub Actions expressions (locked)

- **Interpolation (naming):** `pr-${{ github.repository }}-${{ github.event.number }}`,
  `tkt-${{ linear.data.identifier }}`. Text outside `${{ }}` is literal.
- **Conditional:** a single GHA expression evaluated for truthiness, e.g.
  `linear.action == 'update' && linear.data.state.name == 'In Progress'`.

We hand-write a small, fully-specified evaluator (no maintained Go lib exists for GHA
expressions — depending on an unmaintained one would violate "zero unknowns").

#### Semantics to implement (from the GHA spec)

- **Literals:** `true`, `false`, `null`, numbers (incl. `0x`, exponent), single-quoted strings
  with `''` escaping.
- **Operators & precedence (high→low):** `() [] .` ; `!` ; `< <= > >=` ; `== !=` ; `&&` ; `||`.
- **Loose equality:** coerce non-matching types to number (null→0, false→0, true→1, ""→0,
  numeric string→its number, unparseable/array/object→NaN). **String compares are
  case-insensitive.** NaN in relational ops → false.
- **Truthiness:** falsy = `false, 0, -0, "", null`; everything else truthy.
- **Property/index access:** `a.b`, `a['b']`, `a[0]`, and the `*` filter (`a.*.name`).
- **`&&`/`||` value semantics:** GHA returns the operand value (not a coerced bool); we
  preserve that, and coerce to bool only where a bool is required (the conditional result).
- **Functions:** `contains`, `startsWith`, `endsWith`, `format`, `join`, `toJSON`, `fromJSON`.
- **Deliberately excluded (CI-only, documented):** `hashFiles`, `success`, `always`,
  `cancelled`, `failure`. Calling them is a clear evaluation error, not a silent false.

#### Package surface (`internal/template`)

```go
// Event is the forwarded envelope the templates run against.
type Event struct {
    EventType string         `json:"event_type"` // "linear" | "github"
    Linear    map[string]any `json:"linear,omitempty"`
    GitHub    map[string]any `json:"github,omitempty"`
}

// Engine renders name templates and evaluates boolean conditionals.
type Engine interface {
    // Render expands ${{ ... }} interpolations in tmpl against evt.
    Render(tmpl string, evt Event) (string, error)
    // Evaluate parses a single expression and returns its truthiness.
    Evaluate(expr string, evt Event) (bool, error)
}
```

Internally: a lexer + Pratt/recursive-descent parser → AST → evaluator over `map[string]any`
(the JSON-decoded event). One parser; `Render` just wraps the `${{ }}` scanning around it.

## test_data/ fixtures (the heart of "zero unknowns")

A committed `test_data/` folder of **real captured** webhook payloads, each wrapped in our
envelope and **PII-sanitized** to placeholders (real structure, fake identities).

- **Sources:**
  - GitHub → `octokit/webhooks` `payload-examples/api.github.com/` (official, machine-generated:
    every `issues.*` and `pull_request.*` action).
  - Linear → real captured `Issue` deliveries (e.g. from public webhook-log repos), covering
    create / update (status change, via `updatedFrom`) / remove.
- **Layout:**
  ```
  test_data/
    github/issues.opened.json, issues.labeled.json, pull_request.opened.json, ...
    linear/issue.created.json, issue.status_changed.json, issue.removed.json, ...
  ```
  Each file is the full envelope: `{ "event_type": "linear", "linear": { ...raw... } }`.
- **Sanitize:** names→"Ada Lovelace", emails→"ada@example.com", org ids→"org_sample";
  keep identifiers/state/labels so templates have realistic material.
- **Dual use:** these files are BOTH the Go test fixtures AND the sample-event dropdown in the
  settings test UI (served via an endpoint), so a new user validates templates without ever
  triggering a real Linear event.
- A small scraper script (committed under `scripts/`) fetches + wraps + sanitizes, so the set
  is reproducible/refreshable. Run once; output committed.

## Backend

### Store (`internal/store`)

- **Migration `0005_linear_settings.sql`:** `linear_settings` table keyed by `org_id`:
  - `creation_mode text` ('status' | 'manual')
  - `trigger_status text` (the status name that triggers creation; null for manual)
  - `name_template text`
  - `condition_expr text`
  - `auto_add_bots text[]` (or jsonb)
  - timestamps. PK `(org_id)`.
- CRUD: `GetLinearSettings(orgID)`, `UpsertLinearSettings(...)`.

### Service (`internal/integrations` or new `internal/channelrules`)

- `LinearSettings` get/upsert, gated on Linear workspace connection.
- A `TestTemplate(evt, nameTmpl, condExpr)` helper using `internal/template`, returning
  `{ name, conditionResult, error }` — pure, no side effects.
- Sample-event listing: read `test_data/linear/*` (embedded via `embed.FS`) for the dropdown.

### Spec + API (regen Go + TS)

- `GET /integrations/linear/settings` → current settings (+ available status enum, + sample
  event list).
- `PUT /integrations/linear/settings` → save.
- `POST /integrations/linear/settings/test` → body `{ nameTemplate, condition, event? | sampleId? }`
  → `{ name, conditionResult, error? }`. Accepts a chosen sample event OR pasted JSON.
- Status enum for the dropdown: from Linear's workflow states (best-effort via the stored
  token) or a sensible static fallback set.

## Frontend

- Lives on the **Dashboard** (`/` → `routes/+page.svelte`), as a "Linear workspace settings"
  card/section, shown **only** when Linear is connected at the workspace level (the dashboard
  already fetches integration status via `fetchIntegrationStatus`). Extracted into its own
  component (`lib/components/app/linear-settings.svelte`) to keep `+page.svelte` manageable.
- Controls: creation-mode toggle (status vs manual) + status dropdown; name-template input;
  condition input; bots list editor.
- **Test panel:** pick a sample event (from `test_data/`) or paste JSON → shows rendered name
  + condition true/false + any error. Live validation feedback.
- Icon buttons + tooltips per CLAUDE.md.

## Testing (rigour — the explicit ask)

- **Engine unit tests** (no DB, table-driven):
  - Lexer/parser: every literal, operator precedence, grouping, property/index/`*` filter,
    string `''` escaping, malformed input → error.
  - Equality/coercion matrix: exhaustive cross-type `==`/`!=` cases matching the GHA spec
    (null/bool/number/string/array/object), case-insensitive string compare, NaN relational.
  - Functions: contains/startsWith/endsWith (case-insensitive, casting), format (`{0}`,
    `{{` escaping), join, toJSON/fromJSON round-trip.
  - Excluded functions → explicit error.
- **Fixture-driven tests:** load every `test_data/**` file and assert representative
  name renders + conditions against the REAL github & linear shapes (e.g.
  `linear.data.state.name == 'Done'` on the status_changed fixture is true; an `issues.opened`
  github fixture renders `pr-<repo>-<number>` correctly). This is what proves "github templates
  work" with zero guesswork.
- **Store/service tests:** settings round-trip; TestTemplate returns expected name/condition for
  a known sample.

## Out of scope (next round)

- Acting on the trigger: webhook → evaluate condition → create Slack channel → add bots.
  (Needs Slack channel-create plumbing that doesn't exist yet.)

## Files (indicative)

| File | Change |
|---|---|
| `internal/template/template.go` | Engine interface, Event type |
| `internal/template/lexer.go`, `parser.go`, `eval.go` | GHA expression impl |
| `internal/template/functions.go` | contains/startsWith/format/join/toJSON/fromJSON |
| `internal/template/*_test.go` | exhaustive unit + fixture-driven tests |
| `test_data/github/*.json`, `test_data/linear/*.json` | sanitized real payloads (envelope-wrapped) |
| `scripts/scrape_test_data.*` | reproducible fetch+wrap+sanitize |
| `internal/store/migrations/0005_linear_settings.sql` | new table |
| `internal/store/linear_settings.go` | CRUD |
| `internal/integrations/linear_settings.go` | service: get/upsert/test + sample listing |
| `spec/openapi.yaml` + regenerated `*_gen.go` / `schema.d.ts` | settings + test endpoints |
| `internal/httpapi/handler.go` | handlers |
| `frontend/src/lib/components/app/linear-settings.svelte` | dashboard settings + test card |
| `frontend/src/routes/+page.svelte` | mount the card (gated on Linear workspace connected) |
| `frontend/src/lib/integrations.ts` (or new module) | client helpers |

## Verification

`cd backend && make generate && go build ./... && go test ./...` (engine + fixtures + store);
`cd frontend && npm run build && npm run check`. Manual: pick a sample Linear event, tweak
template/condition, see live name + true/false.

## Commits (on `main`)

1. `template` service + `test_data/` fixtures + exhaustive tests (the reusable, rigorously-tested core).
2. Linear settings: store + service + spec/regen + handlers.
3. Linear settings UI + test panel.
