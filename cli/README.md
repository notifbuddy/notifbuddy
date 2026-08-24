# notifbuddy CLI

Two-way Linear <-> Slack sync, from your terminal.

```
brew install notifbuddy/tap/notifbuddy
```

## Getting started

```
notifbuddy login              # sign in via your browser (device flow)
notifbuddy org create "Acme"  # first run only
notifbuddy connect slack --level workspace   # install the Slack app (opens browser)
notifbuddy connect slack --level user        # your personal Slack connection
notifbuddy connect linear --level workspace  # authorize Linear (opens browser)
notifbuddy connect linear --level user       # your personal Linear connection
notifbuddy settings create --team <teamId> --creation-mode status \
  --trigger-status "In Progress" --name-template 'tkt-${{ linear.issue.identifier }}'
```

Every command accepts `--json` for machine-readable output.

## Agent-driven onboarding

The CLI bundles a skill that teaches Claude, Codex, and other coding agents to
run the whole onboarding conversationally:

```
notifbuddy skill install            # installs into ~/.claude/skills
notifbuddy skill install --agent codex
notifbuddy skill show               # print it for any other agent
```

Then ask your agent to "set up notifbuddy".

## Self-hosting

Point the CLI at your own deployment with `--api-url`, `--auth-url`, and
`--dashboard-url`, the `NOTIFBUDDY_API_URL` / `NOTIFBUDDY_AUTH_URL` /
`NOTIFBUDDY_DASHBOARD_URL` environment variables, or
`~/.config/notifbuddy/config.yaml`:

```yaml
api_url: https://api.example.com
auth_url: https://auth.example.com
dashboard_url: https://dashboard.example.com
```

## Development

Source lives in the [notifbuddy monorepo](https://github.com/notifbuddy/notifbuddy)
under `cli/`. The typed API client is generated from `spec/openapi.yaml`
(`make gen-cli`); build with `make build-cli`.

The CLI reads a `.env` next to the binary. For testing against a local stack,
drop `NOTIFBUDDY_ENV=local` in `cli/bin/.env` — defaults switch to
localhost:8080/8787/5173 while the installed prod binary stays untouched.
