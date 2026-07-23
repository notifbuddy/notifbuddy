#!/usr/bin/env bash
# Rewrite config/{backend,authd,dashboard}/preview.yaml for a PR preview
# before docker build. Usage: preview-bake-configs.sh <pr_number>
set -euo pipefail

die() { echo "preview-bake: $*" >&2; exit 1; }
need() { command -v "$1" >/dev/null || die "need $1"; }

need yq

pr="${1:-}"
zone="${PREVIEW_ZONE:-notifbuddy.com}"
root="$(cd "$(dirname "$0")/.." && pwd)"

[[ "$pr" =~ ^[0-9]+$ ]] || die "pr_number must be a positive integer"

api="https://api-pr-${pr}.${zone}"
auth="https://auth-pr-${pr}.${zone}"
dash="https://dashboard-pr-${pr}.${zone}"
prefix="better-auth-pr-${pr}"

backend="${root}/config/backend/preview.yaml"
authd="${root}/config/authd/preview.yaml"
dashboard="${root}/config/dashboard/preview.yaml"
[ -f "$backend" ] || die "missing $backend"
[ -f "$authd" ] || die "missing $authd"
[ -f "$dashboard" ] || die "missing $dashboard"

yq -i "
  .server.public_base_url = \"${api}\" |
  .cors.allow_origin = \"${dash}\" |
  .app.post_login_url = \"${dash}\" |
  .pubsub.gcp.push_audience = \"${api}/internal/pubsub/push\" |
  .slack.callback_url = \"${api}/integrations/slack/callback\" |
  .linear.callback_url = \"${api}/integrations/linear/callback\" |
  .logging.axiom_enabled = false |
  .otel.enabled = false
" "$backend"

yq -i "
  .auth.base_url = \"${auth}\" |
  .auth.cookie_domain = \".${zone}\" |
  .auth.cookie_prefix = \"${prefix}\" |
  .cors.trusted_origins = [\"${dash}\"]
" "$authd"

yq -i "
  .api_base_url = \"${api}\" |
  .auth_url = \"${auth}\" |
  .better_stack_token = \"\"
" "$dashboard"

echo "preview-bake: api=${api} auth=${auth} dash=${dash} cookie_prefix=${prefix}"
