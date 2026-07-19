# --- build -----------------------------------------------------------------
# Pinned to the build platform and cross-compiled below: the self-host release
# publishes arm64 too, and Go cross-compiles in seconds where emulating the
# whole toolchain under QEMU takes minutes. Both are no-ops for a plain
# single-platform `docker build`.
FROM --platform=$BUILDPLATFORM golang:1.25.8-alpine AS build
WORKDIR /src

# Cache deps first.
COPY go.mod go.sum ./
RUN go mod download

# Build a static binary (no cgo) for a minimal runtime image. TARGETOS/TARGETARCH
# are supplied by buildx and default to the build platform when unset.
ARG TARGETOS
ARG TARGETARCH
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
	go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# --- runtime ---------------------------------------------------------------
FROM alpine:3.20
# TLS roots for outbound HTTPS (Slack/Linear/GitHub/Neon).
RUN apk add --no-cache ca-certificates
WORKDIR /app

COPY --from=build /out/server /app/server
# Ship both configs; the image runs the prod one (CONFIG_FILE selects it).
COPY --from=build /src/config.prod.yaml /app/config.prod.yaml
COPY --from=build /src/config.local.yaml /app/config.local.yaml
ENV CONFIG_FILE=config.prod.yaml

# Runs unprivileged. Clusters that enforce the restricted Pod Security
# Standard reject a container that could run as uid 0, and OpenShift assigns
# an arbitrary non-root uid, so an image that needs root simply won't start
# there. Nothing here does: the binary is static, the port is above 1024, and
# every file it reads is world-readable.
USER 65532:65532

# Cloud Run sends traffic to $PORT (defaults to 8080); the config listens on :8080.
EXPOSE 8080
ENTRYPOINT ["/app/server"]
