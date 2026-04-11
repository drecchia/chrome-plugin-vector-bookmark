# ── Stage 1: Build ────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

WORKDIR /src

# Cache dependency layer separately
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# modernc.org/sqlite is pure Go — CGO_ENABLED=0 produces a fully static binary
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /bin/vbmd \
    ./cmd/vbmd/

# ── Stage 2: Runtime ──────────────────────────────────────────────────────────
FROM alpine:3.20

# ca-certificates: needed if daemon ever makes outbound TLS calls (future sync)
# tzdata: correct timestamps in logs
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S vbm && adduser -S -G vbm vbm

COPY --from=builder /bin/vbmd /usr/local/bin/vbmd

# Data directory — must match the volume mount in docker-compose.yml
# session.json, vbm.db and snapshots live here
RUN mkdir -p /home/vbm/.local/share/vbm && \
    chown -R vbm:vbm /home/vbm

USER vbm
WORKDIR /home/vbm

# The daemon picks a random available port at runtime and writes it to
# session.json. No fixed EXPOSE — use network_mode: host so the host's
# Chrome extension can reach 127.0.0.1:PORT directly.

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:$(grep -o '"port":[0-9]*' /home/vbm/.local/share/vbm/session.json | grep -o '[0-9]*')/healthz || exit 1

ENTRYPOINT ["vbmd", "server"]
