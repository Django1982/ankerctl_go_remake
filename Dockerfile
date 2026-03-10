# =============================================================================
# ankerctl -- Multi-stage Docker Build
# =============================================================================
# Build:  docker build -t ankerctl .
# Run:    docker run --network host -v ~/.ankerctl:/root/.ankerctl ankerctl
# =============================================================================

# ---------------------------------------------------------------------------
# Stage 1: Build the Go binary
# ---------------------------------------------------------------------------
FROM golang:1.24-alpine AS builder

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

RUN apk add --no-cache ffmpeg ca-certificates tzdata \
    && adduser -D -h /home/ankerctl ankerctl

COPY --from=builder /out/ankerctl /usr/local/bin/ankerctl

# Config and captures directories
RUN mkdir -p /root/.ankerctl /captures \
    && chown -R ankerctl:ankerctl /home/ankerctl

# Static files are embedded via //go:embed -- no COPY needed.

EXPOSE 4470

ENTRYPOINT ["ankerctl", "webserver", "--listen", "0.0.0.0:4470"]
