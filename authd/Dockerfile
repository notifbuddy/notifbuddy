FROM node:24-slim AS deps
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev

FROM node:24-slim
WORKDIR /app
ENV NODE_ENV=production
COPY --from=deps /app/node_modules ./node_modules
COPY package.json config.prod.yaml ./
COPY src ./src
ENV CONFIG_FILE=config.prod.yaml
EXPOSE 8787
CMD ["node", "src/server.ts"]
