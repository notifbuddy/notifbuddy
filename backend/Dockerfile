# Dockerfile for the Go backend. Build from the **repo root**:
#   docker build -f backend/Dockerfile .
#
# --- build -----------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.25.8-alpine AS build
WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN go mod download

ARG TARGETOS
ARG TARGETARCH
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
	go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# --- runtime ---------------------------------------------------------------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app

COPY --from=build /out/server /app/server
COPY config /app/config
ENV NB_ENV=prod
ENV CONFIG_FILE=config/backend/prod.yaml

USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app/server"]
