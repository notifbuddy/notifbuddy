# Dockerfile for authd. Build from the **repo root**:
#   docker build -f authd/Dockerfile .
FROM node:24-slim AS deps
WORKDIR /app
COPY authd/package.json authd/package-lock.json ./
RUN npm ci --omit=dev

FROM node:24-slim
WORKDIR /app
ENV NODE_ENV=production
COPY --from=deps /app/node_modules ./node_modules
COPY authd/package.json ./
COPY authd/src ./src
COPY config /app/config
ENV NB_ENV=prod
ENV CONFIG_FILE=config/authd/prod.yaml
ENV FEATUREFLAGS_FILE=config/featureflags/prod.yaml
ENV npm_config_cache=/tmp/.npm
ENV HOME=/tmp
USER 1000:1000
EXPOSE 8787
CMD ["node", "src/server.ts"]
