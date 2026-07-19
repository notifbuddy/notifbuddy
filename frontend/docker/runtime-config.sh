#!/bin/sh
# Bake the API/authd origins into the SPA shell at container start.
#
# The dashboard is a static bundle, so the origins can't be known when the
# image is built — one published image serves every self-hosted domain. The
# build leaves a marker line in index.html (frontend/src/app.html); this script
# replaces that line with the real config before nginx serves anything.
#
# nginx's entrypoint runs everything in /docker-entrypoint.d in order, and any
# non-zero exit aborts startup — so a misconfigured dashboard fails loudly here
# rather than serving a shell that 404s every API call.
set -eu

INDEX=/usr/share/nginx/html/index.html
MARKER='notifbuddy:runtime-config'

: "${API_BASE_URL:?dashboard: API_BASE_URL is required (the backend origin, e.g. https://api.example.com)}"
: "${AUTH_URL:?dashboard: AUTH_URL is required (the authd origin, e.g. https://auth.example.com)}"

# The values land inside a JSON string literal in a <script> block. Rather than
# escape them, refuse anything that could break out of it — these are URLs, so
# a quote or backslash means the value is wrong, not that it needs quoting.
for value in "$API_BASE_URL" "$AUTH_URL"; do
	case $value in
	*[\"\\\<]*)
		echo "dashboard: refusing URL containing a quote, backslash or angle bracket: $value" >&2
		exit 1
		;;
	esac
done

# Exactly one marker, or the build changed shape underneath us and a blind
# substitution would silently do the wrong thing.
count=$(grep -c "$MARKER" "$INDEX" || true)
if [ "$count" != "1" ]; then
	echo "dashboard: expected exactly 1 '$MARKER' marker in index.html, found $count" >&2
	exit 1
fi

config=$(printf '{"apiBaseUrl":"%s","authUrl":"%s"}' "$API_BASE_URL" "$AUTH_URL")

# awk, not sed: no delimiter or backslash quoting to get wrong in the URLs.
#
# Staged through /tmp and written back by truncating the existing file, rather
# than the usual write-tmp-then-rename: a rename needs the html directory
# writable, and this way only index.html itself is, with the rest of the
# document root staying root-owned and read-only.
awk -v marker="$MARKER" -v line="		<script>window.__notifbuddy = $config;</script>" \
	'index($0, marker) { print line; next } { print }' \
	"$INDEX" >/tmp/index.html
cat /tmp/index.html >"$INDEX"
rm -f /tmp/index.html

echo "dashboard: runtime config set (api=$API_BASE_URL auth=$AUTH_URL)"
