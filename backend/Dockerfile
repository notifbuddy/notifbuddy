# Dockerfile for the Go backend. Build from the **repo root**:
#   docker build -f backend/Dockerfile .
#
# --- build -----------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine AS build
WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN go mod download

ARG TARGETOS
ARG TARGETARCH
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
	go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# --- runtime ---------------------------------------------------------------
FROM alpine:3.24
RUN apk add --no-cache ca-certificates
WORKDIR /app

COPY --from=build /out/server /app/server
# No default CONFIG_FILE or NB_ENV: every deployment supplies its own backend
# config — the chart mounts a ConfigMap, e2e mounts config/backend/e2e.yaml,
# and the Cloud Run preview sets CONFIG_FILE to the baked preview.yaml below.
# Starting the image without one fails naming the file it looked for, which
# beats silently booting somebody else's environment.
COPY config/backend/preview.yaml /app/config/backend/preview.yaml

USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app/server"]
