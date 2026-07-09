# --- build -----------------------------------------------------------------
FROM golang:1.25.2-alpine AS build
WORKDIR /src

# Cache deps first.
COPY go.mod go.sum ./
RUN go mod download

# Build a static binary (no cgo) for a minimal runtime image.
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# --- runtime ---------------------------------------------------------------
FROM alpine:3.20
# TLS roots for outbound HTTPS (Neon requires TLS).
RUN apk add --no-cache ca-certificates
WORKDIR /app

COPY --from=build /out/server /app/server
# Ship both configs; the image runs the prod one (CONFIG_FILE selects it).
COPY --from=build /src/config.prod.yaml /app/config.prod.yaml
COPY --from=build /src/config.local.yaml /app/config.local.yaml
ENV CONFIG_FILE=config.prod.yaml

# Cloud Run sends traffic to $PORT (defaults to 8080); the config listens on :8080.
EXPOSE 8080
ENTRYPOINT ["/app/server"]
