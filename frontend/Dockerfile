# Self-hosted dashboard image: the SvelteKit SPA behind nginx.
#
# Our own dashboard doesn't use this — it deploys to Cloudflare as a Worker
# with static assets (wrangler.jsonc). This image exists for the Helm chart,
# where the API/authd origins differ per install and so can't be baked in.
# docker/runtime-config.sh writes them into the shell at container start.
#
# Build context is the REPO ROOT: the SPA needs frontend/ and the repo-root
# shared/ directory (aliased as $shared, plus the shared error template).
#
#   docker build -f frontend/Dockerfile -t notifbuddy/dashboard .

# --- build -----------------------------------------------------------------
# Pinned to the build platform: the output is static files, identical whatever
# the target architecture, so there is no reason to run npm under emulation
# when building the arm64 image.
FROM --platform=$BUILDPLATFORM node:24-bookworm-slim AS build
WORKDIR /app/frontend

# Deps first, for layer caching.
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

COPY shared /app/shared
COPY config /app/config
COPY frontend ./

# $env/static/public is resolved at build time and an undefined variable is a
# build error, so both are defined and empty: src/lib/runtime-config.ts then
# falls through to window.__notifbuddy, which runtime-config.sh fills in.
# Feature flags still come from config/featureflags/${NB_ENV}.yaml.
ENV NB_ENV=prod
ENV PUBLIC_API_BASE_URL=""
ENV PUBLIC_AUTH_URL=""
RUN npm run build

# --- runtime ---------------------------------------------------------------
# The unprivileged variant runs as uid 101 and listens on 8080, so the chart
# needs no securityContext gymnastics to satisfy a restricted PodSecurity level.
FROM nginxinc/nginx-unprivileged:1.29-alpine

COPY frontend/docker/nginx.conf /etc/nginx/conf.d/default.conf
# nginx's entrypoint runs this before starting; a non-zero exit aborts startup.
COPY frontend/docker/runtime-config.sh /docker-entrypoint.d/40-runtime-config.sh
COPY --from=build /app/frontend/build /usr/share/nginx/html

USER root
RUN chmod +x /docker-entrypoint.d/40-runtime-config.sh \
	# The entrypoint rewrites the shell in place as uid 101, so that one file
	# has to be writable by it. Everything else stays root-owned and read-only.
	&& chown 101:101 /usr/share/nginx/html/index.html
USER 101

EXPOSE 8080
