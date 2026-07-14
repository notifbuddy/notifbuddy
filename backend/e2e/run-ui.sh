#!/usr/bin/env bash
# Build and run the dashboard (SvelteKit SPA) Playwright suite in docker compose
# against the real backend stack. Exits with the Playwright runner's status code
# (0 = all green). Extra args pass through to compose.
#
#   ./run-ui.sh              # run everything
#   ./run-ui.sh --no-build   # skip rebuilds
#
# Same stack as run.sh (postgres + fakeapis + backend), but runs the "ui"
# profile instead of the backend Go suite. The browser authenticates with the
# session cookie fakeapis forges onto the shared certs volume.
set -euo pipefail
cd "$(dirname "$0")"

COMPOSE="docker compose -f docker-compose.e2e.yml"

cleanup() { $COMPOSE down -v --remove-orphans >/dev/null 2>&1 || true; }
trap cleanup EXIT

$COMPOSE --profile ui up --build --abort-on-container-exit --exit-code-from ui "$@"
