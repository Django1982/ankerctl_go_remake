# =============================================================================
# ankerctl -- Multi-stage Docker Build
# =============================================================================
# Build:  docker build -t ankerctl .
# Run:    docker run --network host -v ~/.ankerctl:/root/.ankerctl ankerctl
# =============================================================================

# ---------------------------------------------------------------------------
# Stage 1: Build the Go binary
# ---------------------------------------------------------------------------
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git curl bash

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN bash scripts/prepare-web-vendor.sh
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags "-s -w -X main.Version=${VERSION}" \
    -o /out/ankerctl \
    ./cmd/ankerctl/

# ---------------------------------------------------------------------------
# Stage 2: Minimal runtime image
# ---------------------------------------------------------------------------
FROM alpine:latest

RUN apk add --no-cache ffmpeg ca-certificates tzdata su-exec \
    && adduser -D -h /home/ankerctl ankerctl

COPY --from=builder /out/ankerctl /usr/local/bin/ankerctl

COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

# Config and captures directories
RUN mkdir -p /root/.ankerctl /captures /logs \
    && chown -R ankerctl:ankerctl /home/ankerctl /captures /logs

# Static files are embedded via //go:embed -- no COPY needed.

EXPOSE 4470

STOPSIGNAL SIGINT

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:4470/api/health || exit 1

ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["ankerctl", "webserver", "--listen", "0.0.0.0:4470"]
