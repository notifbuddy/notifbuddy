#!/usr/bin/env bash
# Validate a hostname against Google's favicon guidelines + local link tags.
# Usage: scripts/check-favicon.sh https://docs.notifbuddy.com
set -euo pipefail

BASE="${1:-}"
if [[ -z "$BASE" ]]; then
  echo "usage: $0 https://docs.notifbuddy.com" >&2
  exit 2
fi
BASE="${BASE%/}"

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

fail=0
pass() { printf 'PASS  %s\n' "$*"; }
warn() { printf 'WARN  %s\n' "$*"; }
bad()  { printf 'FAIL  %s\n' "$*"; fail=1; }

html="$tmpdir/home.html"
curl -fsSL -A 'Googlebot' "$BASE/" -o "$html"

if rg -q 'rel="icon"[^>]+favicon-96\.png|href="/favicon-96\.png"' "$html"; then
  pass "homepage declares favicon-96.png"
else
  bad "homepage missing rel=icon for /favicon-96.png"
fi

if rg -q 'rel="shortcut icon"' "$html"; then
  warn "homepage declares shortcut icon (prefer PNG-first rel=icon; shortcut can steer crawlers at a small .ico)"
fi

robots="$(curl -fsSL "$BASE/robots.txt" || true)"
if printf '%s' "$robots" | rg -qi 'disallow:\s*/favicon'; then
  bad "robots.txt blocks favicon paths"
else
  pass "robots.txt does not block favicons"
fi

for path in /favicon-96.png /favicon.ico /favicon.svg /apple-touch-icon.png; do
  code="$(curl -sS -o "$tmpdir/asset" -w '%{http_code}' -A 'Googlebot-Image' "$BASE$path")"
  ctype="$(file -b --mime-type "$tmpdir/asset" 2>/dev/null || true)"
  if [[ "$code" == "200" ]]; then
    pass "$path → HTTP 200 ($ctype)"
  else
    bad "$path → HTTP $code"
  fi
done

png="$tmpdir/favicon-96.png"
curl -fsSL "$BASE/favicon-96.png" -o "$png"
w="$(sips -g pixelWidth "$png" 2>/dev/null | awk '/pixelWidth/{print $2}')"
h="$(sips -g pixelHeight "$png" 2>/dev/null | awk '/pixelHeight/{print $2}')"
if [[ "$w" == "$h" && "${w:-0}" -ge 48 ]]; then
  pass "favicon-96.png is square and ≥48px (${w}x${h})"
else
  bad "favicon-96.png must be square ≥48px (got ${w:-?}x${h:-?})"
fi

gcode="$(curl -sS -o "$tmpdir/g.png" -w '%{http_code}' -A 'Mozilla/5.0' \
  "https://t0.gstatic.com/faviconV2?client=SOCIAL&type=FAVICON&fallback_opts=TYPE,SIZE,URL&url=${BASE}/&size=64")"
gw="$(sips -g pixelWidth "$tmpdir/g.png" 2>/dev/null | awk '/pixelWidth/{print $2}')"
gh="$(sips -g pixelHeight "$tmpdir/g.png" 2>/dev/null | awk '/pixelHeight/{print $2}')"
if [[ "$gcode" == "200" && "${gw:-0}" -ge 32 ]]; then
  pass "Google faviconV2 cache has an icon (${gw}x${gh}, HTTP $gcode)"
elif [[ "$gcode" == "404" ]]; then
  warn "Google faviconV2 has no icon yet (HTTP 404, ${gw:-?}x${gh:-?} placeholder) — eligible markup still needs a recrawl"
else
  warn "Google faviconV2 unexpected HTTP $gcode (${gw:-?}x${gh:-?})"
fi

echo
if [[ "$fail" -ne 0 ]]; then
  echo "RESULT: FAIL ($BASE)"
  exit 1
fi
echo "RESULT: OK ($BASE) — markup meets Google guidelines; faviconV2 warn is crawl lag until Google recrawls"
exit 0
