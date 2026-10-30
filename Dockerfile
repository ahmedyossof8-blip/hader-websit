# ==============================================================================
# HADER-V2: Ultra-Lightweight Multi-Stage Production Container (< 20MB)
# ==============================================================================

# ------------------------------------------------------------------------------
# Stage 1: Build Environment (Optimized dependency caching & static compilation)
# ------------------------------------------------------------------------------
FROM golang:1.23-alpine AS builder

# Install build dependencies for static cross-compilation
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /src

# Leverage Docker layer caching for Go modules
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy application source code
COPY . .

# Compile fully static Linux ELF binary with stripped debug symbols and trimmed paths
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s -extldflags '-static'" \
    -trimpath \
    -tags timetzdata \
    -o /app/server ./cmd/api/main.go

# ------------------------------------------------------------------------------
# Stage 2: Hardened Minimal Runtime (Alpine Linux 3.20, Non-Root UID 10001)
# ------------------------------------------------------------------------------
FROM alpine:3.20

# Install runtime essentials: CA roots for TLS & timezone data
RUN apk add --no-cache ca-certificates tzdata wget \
    && cp /usr/share/zoneinfo/Africa/Cairo /etc/localtime \
    && echo "Africa/Cairo" > /etc/timezone \
    && apk del tzdata \
    && rm -rf /var/cache/apk/*

# Enforce Least-Privilege: Unprivileged system user without login shell
RUN addgroup -g 10001 -S appuser \
    && adduser -u 10001 -S appuser -G appuser -s /sbin/nologin

WORKDIR /app

# Copy stripped binary from builder stage
COPY --from=builder --chown=appuser:appuser /app/server /app/server

# Security: Run as non-root user
USER 10001:10001

# Healthcheck probing Fiber's /health endpoint dynamically adapting to $PORT
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q --spider "http://127.0.0.1:${PORT:-8080}/health" || exit 1

EXPOSE 8080

ENTRYPOINT ["/app/server"]
