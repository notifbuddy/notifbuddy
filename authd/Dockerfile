FROM node:24-slim AS deps
WORKDIR /app
COPY package.json package-lock.json ./
# @better-auth/cli is a runtime dependency on purpose: the deploy pipeline's
# migrate job runs `npm run migrate` inside this same image.
RUN npm ci --omit=dev

FROM node:24-slim
WORKDIR /app
ENV NODE_ENV=production
COPY --from=deps /app/node_modules ./node_modules
COPY package.json config.prod.yaml ./
COPY src ./src
ENV CONFIG_FILE=config.prod.yaml

# Runs unprivileged, as the "node" user the base image already ships (uid
# 1000). Clusters enforcing the restricted Pod Security Standard reject
# containers that could run as uid 0, and OpenShift assigns an arbitrary
# non-root uid.
#
# npm wants a writable cache and HOME, and neither is writable for this user
# once we drop root — point both at /tmp so `npm run migrate` works.
ENV npm_config_cache=/tmp/.npm
ENV HOME=/tmp
# Numeric, not the "node" name it corresponds to: with runAsNonRoot set,
# kubelet refuses to start a container whose image declares a non-numeric
# user, because it cannot check that the name isn't root.
USER 1000:1000
EXPOSE 8787
CMD ["node", "src/server.ts"]
