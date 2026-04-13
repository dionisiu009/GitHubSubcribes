# =============================================================================
# Stage 1: base — common Go environment
# =============================================================================
FROM golang:1.21-alpine AS base

# Install essential build tools and CA certs (needed for HTTPS calls)
RUN apk add --no-cache git ca-certificates tzdata make curl

WORKDIR /app

# Cache dependency download layer separately from source build
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# =============================================================================
# Stage 2: development — includes Air for live-reload
# =============================================================================
FROM base AS development

# Install Air for hot-reload in dev
RUN go install github.com/air-verse/air@latest

COPY . .

EXPOSE 8080

# Air reads .air.toml if present, otherwise uses defaults
CMD ["air", "-c", ".air.toml"]

# =============================================================================
# Stage 3: builder — compiles the production binary
# =============================================================================
FROM base AS builder

COPY . .

# Build a statically linked binary (no CGO, no external libs needed)
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo 'dev')" \
    -trimpath \
    -o /app/bin/server \
    ./cmd/server

# =============================================================================
# Stage 4: production — minimal distroless image
# =============================================================================
FROM gcr.io/distroless/static-debian12 AS production

# Copy timezone data and CA certs from builder
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy only the compiled binary
COPY --from=builder /app/bin/server /server

# Copy migrations so the app can run them on startup
COPY --from=builder /app/migrations /migrations

EXPOSE 8080

# Run as non-root user for security
USER nonroot:nonroot

ENTRYPOINT ["/server"]
