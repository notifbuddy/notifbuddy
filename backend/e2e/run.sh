#!/usr/bin/env bash
# Build and run the backend e2e suite in docker compose. Exits with the test
# runner's status code (0 = all green). Extra args pass through to compose.
#
#   ./run.sh                 # run everything
#   ./run.sh --no-build      # skip rebuilds
set -euo pipefail
cd "$(dirname "$0")"

COMPOSE="docker compose -f docker-compose.e2e.yml"

cleanup() { $COMPOSE down -v --remove-orphans >/dev/null 2>&1 || true; }
trap cleanup EXIT

$COMPOSE --profile backend up --build --abort-on-container-exit --exit-code-from tests "$@"
