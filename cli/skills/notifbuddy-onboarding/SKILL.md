---
name: notifbuddy-onboarding
description: Guide a user through setting up notifbuddy (two-way Linear <-> Slack sync) using the notifbuddy CLI. Use when the user asks to set up, onboard, configure, or connect notifbuddy, or to sync Linear with Slack.
---

# notifbuddy onboarding

You are guiding a human through connecting Linear and Slack with notifbuddy. The
`notifbuddy` CLI does the work; your job is to run its commands, read the JSON
output, explain what is happening in one or two sentences per step, and ask the
user only the questions listed here.

Ground rules:

- Always pass `--json` and read the structured output; never scrape the human text.
- Commands that open a browser (`login`, `connect`) block until the user finishes
  in the browser. Tell the user to look at their browser BEFORE running the
  command, then run it and wait.
- Never ask the user to copy tokens or secrets. Sign-in and OAuth all happen in
  the browser.
- If any command fails with "not logged in", run `notifbuddy login` and resume.
- Self-hosted installs: `--api-url`, `--auth-url`, and `--dashboard-url` flags
  (or `NOTIFBUDDY_API_URL` etc.) point the CLI at the user's own deployment; ask
  for those URLs first if the user says they self-host.

## Step 0 — check the CLI

Run `notifbuddy --version`. If the binary is missing, have the user install it:

```
brew install notifbuddy/tap/notifbuddy
```

## Step 1 — sign in

Tell the user a browser window will open where they confirm a one-time code,
then run:

```
notifbuddy login --json
```

The command prints the code and URL on stderr, opens the browser, and blocks
until approved (15-minute limit). On success the JSON has `status: "logged_in"`,
`email`, and `organizationId` (empty string if none yet).

## Step 2 — organization

Run `notifbuddy whoami --json`.

- If `organizationId` is set: confirm the org name with the user and continue.
- If empty and `organizations` is non-empty: show the names, ask which to use,
  then `notifbuddy org switch <name> --json`.
- If the user has no orgs: ask what their organization should be called (usually
  the company/team name), then `notifbuddy org create "<name>" --json`.

## Step 3 — connect Slack (workspace AND user — both required)

`connect` always takes an explicit `--level`; there is no default. Onboarding
is not complete until BOTH levels are connected for BOTH providers.

First the workspace level — this installs the notifbuddy app into their Slack
workspace so it can create and sync channels. The browser opens Slack's consent
page; they may need a Slack workspace admin:

```
notifbuddy connect slack --level workspace --json
```

The command opens the browser and polls until connected (5-minute default).
The JSON result includes the connected Slack team in `account`.

Then the user level — the workspace connection acts as the notifbuddy bot; the
personal connection makes messages and reactions this user writes appear as
*them* on the other side:

```
notifbuddy connect slack --level user --json
```

## Step 4 — connect Linear (workspace AND user — both required)

Workspace level authorizes notifbuddy against their Linear workspace
(read/write) so issue events flow in and comments flow back; user level
attributes their actions to them:

```
notifbuddy connect linear --level workspace --json
notifbuddy connect linear --level user --json
```

Teammates connect their own user level later from the dashboard (Settings →
Integrations → User tab) — mention that at wrap-up.

## Step 5 — sync and inspect

```
notifbuddy sync --json
notifbuddy settings list --json
```

`settings list` returns everything needed to build configs:

- `teams[]` — Linear teams with `teamId`, `teamName`, and their workflow
  `states[]` (names like "Todo", "In Progress", "Done").
- `slackMembers[]` — Slack members and bots with ids (`U…`) for auto-add.
- `sampleEvents[]` — sample event ids for dry-runs.
- `configs[]` — existing configs (a team can have at most ONE config).

## Step 6 — create the first channel-creation config

Ask the user, in plain language, per Linear team they care about:

1. **Which team?** (map name → `teamId` from `teams[]`)
2. **How should channels get created?** Present exactly this choice first:
   - **Manual — less noisy.** A channel is created only when someone asks:
     @notifbuddy on the issue, or you (the agent) on their request. Nothing
     appears in Slack until it's wanted. → `--creation-mode manual`
   - **Auto-create — more noisy.** A channel is created for every issue that
     meets a trigger, with no one asking. Good for teams that want a channel
     per active issue; expect more channels in Slack.
   If they pick auto-create, then ask which trigger:
   - "when it reaches a status" → `--creation-mode status --trigger-status "<state name>"`
   - anything more specific → `--creation-mode condition --condition '<expr>'`,
     where YOU write the expression from their plain-language rule (see
     "Writing condition expressions" below). Never ask the user for an
     expression.
3. **What should channels be named?** → `--name-template`. Templates use
   GitHub-Actions expression syntax over the Linear event, e.g.
   `tkt-${{ linear.issue.identifier }}`. Useful fields:
   `linear.issue.identifier`, `linear.issue.title`, `linear.issue.team.key`,
   `linear.issue.state.name`, `linear.action`. Slack lowercases channel names.
4. **When should the channel be archived?** Same three modes:
   `--archive-mode status --archive-status "Done"`, or
   `--archive-mode condition --archive-condition '<expr>'`, or manual (default).
5. **Who should be auto-added to new channels?** Map names → ids from
   `slackMembers[]`, pass `--auto-add U123,U456`.

Then create it:

```
notifbuddy settings create --team <teamId> --creation-mode status \
  --trigger-status "In Progress" --name-template 'tkt-${{ linear.issue.identifier }}' \
  --archive-mode status --archive-status "Done" --auto-add U123 --json
```

### Writing condition expressions

Users describe rules in plain language; you translate them into the expression
and prove it with `settings test` before saving. The loop:

1. **Elicit.** Take their sentence, e.g. "only non-marketing issues get
   channels".
2. **Clarify what encodes the concept.** "Marketing" could be a label, a team,
   a project, or a word in the title — ask: "How is an issue marked as
   marketing — a label, the team, something in the title?" User: "it's a
   label".
3. **Check the real shape.** `settings list` returns `sampleEvents[].raw`, the
   full event envelope JSON — read one to see the exact fields before writing
   the expression. Labels live at `linear.issue.labels` (objects with a
   `name`), state at `linear.issue.state.name` / `.type`, priority at
   `linear.issue.priorityLabel`, event kind at `linear.action`
   (create/update/remove) and `linear.type`.
4. **Write it.** For "no marketing label":
   `!contains(join(linear.issue.labels.*.name), 'marketing')`
5. **Test it.** Run `settings test --condition '<expr>' --sample <id> --json`
   and check `wouldCreate` flips the way the user expects. If they have a real
   issue in mind, test that too (`--event-file` with an envelope from
   `notifbuddy webhooks --json`). Show the user the outcome in their words:
   "an issue labeled marketing would NOT get a channel; others would."
6. **Iterate** until the dry-runs match their intent, then save the config.

Expression language (GitHub-Actions syntax, over the event envelope):

- Operators: `==`, `!=`, `<`, `<=`, `>`, `>=`, `&&`, `||`, `!`, parentheses.
  String comparisons are case-insensitive.
- Property access: `linear.issue.state.name`, index `[0]`, and the
  object-filter `.*` (e.g. `linear.issue.labels.*.name` → array of names).
- Functions: `contains(search, item)` (string or array), `startsWith(s, p)`,
  `endsWith(s, p)`, `format('{0}-{1}', a, b)`, `join(array, sep)`,
  `fromJSON(s)`.
- Examples:
  - urgent only: `linear.issue.priorityLabel == 'Urgent'`
  - specific label: `contains(linear.issue.labels.*.name, 'incident')`
  - completed states: `linear.issue.state.type == 'completed'` (archive
    condition)
  - title keyword: `contains(linear.issue.title, 'customer')`

## Step 7 — test before trusting

Dry-run a draft against a sample event BEFORE creating it, and show the user
the result:

```
notifbuddy settings test --sample issue.status_changed \
  --creation-mode status --trigger-status "In Progress" \
  --name-template 'tkt-${{ linear.issue.identifier }}' --json
```

The JSON reports the rendered `name`, `wouldCreate`, `wouldArchive`, and any
template `error`. If there is an error, fix the template and re-test before
saving changes with `settings update`.

After configs are saved, validate them in one shot:

```
notifbuddy settings validate --json
```

This runs every saved config against every sample event and exits non-zero if
any template or condition has an error — the JSON lists per-config, per-sample
results (`ok`, rendered `name`, `wouldCreate`, `wouldArchive`, `error`). Use it
as the final gate of onboarding, and any time after editing configs. To re-test
one saved config against one event, `settings test` also accepts
`--setting-id <id>` (flags you pass override the saved fields).

## Step 8 — wrap up

Confirm with `notifbuddy status --json` that both providers show
`connected: true` at BOTH the workspace and user levels (four entries) and that
`notifbuddy settings validate --json` passes, then tell the user:

- The dashboard lives at https://dashboard.notifbuddy.com (self-host: their own
  URL) — team members sign in there with GitHub and connect their personal
  Slack/Linear from Settings → Integrations.
- Docs: https://docs.notifbuddy.com
- To watch events arrive: `notifbuddy webhooks --json`.

## Troubleshooting

- `connect` timed out → the browser flow was not finished; re-run the command.
- Browser shows the "interrupted" page with a code:
  - `no_org` → the browser session has no active org; have the user sign in to
    the dashboard with GitHub first, then re-run `connect`.
  - `billing_locked` → trial ended; billing is handled in the dashboard.
  - `invalid_state` → the link expired or a different browser finished the flow;
    re-run `connect` and finish in the browser that opens.
- `settings create` fails with a conflict → that team already has a config; use
  `settings update <settingId>` (ids from `settings list`).
- Session expired → `notifbuddy login` again.
