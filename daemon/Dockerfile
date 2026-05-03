# syntax=docker/dockerfile:1.7

# ── build stage ───────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS build

WORKDIR /src

# Cache go.mod/sum first so dep download is reused across rebuilds.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Run unit tests inside the build stage so a broken commit can never produce
# a published image. Re-runs only when source actually changed (Docker layer
# cache invalidation on the COPY above).
RUN go test ./...

# Pure-Go build (modernc.org/sqlite is CGO-free). Static binary so it runs on
# any minimal base.
ENV CGO_ENABLED=0 \
    GOOS=linux

RUN go build -trimpath -ldflags="-s -w" -o /out/vbmd ./cmd/vbmd

# ── runtime stage ─────────────────────────────────────────────────────────────
# Alpine gives a real shell + apk for debugging while staying small (~7MB).
# ca-certificates is required for HTTPS calls to embedding/LLM providers;
# tzdata so timestamp-based logs render in the deployment timezone.
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -S vbm -g 1000 \
 && adduser  -S -u 1000 -G vbm -h /home/vbm vbm \
 && mkdir -p /data \
 && chown vbm:vbm /data

# Container-friendly defaults baked into the image. Override via -e or the
# compose `environment:` block when needed. Secrets (API keys) should NEVER
# be defaulted here — set them in deployment env / env_file.
ENV VBM_PORT=7532 \
    VBM_BIND=0.0.0.0 \
    VBM_DATA_DIR=/data \
    VBM_LOG_LEVEL=info
# Optional, set in deployment env:
#   VBM_TTL_DAYS=90
#   VBM_EMBED_URL=https://...
#   VBM_EMBED_API_KEY=...
#   VBM_LLM_MODEL=gpt-4o-mini
#   VBM_LLM_PROMPT_SUMMARIZE_FILE=/path/to/summarize.md
#   VBM_LLM_PROMPT_SUGGEST_TAGS_FILE=/path/to/tags.md

VOLUME ["/data"]
EXPOSE 7532

COPY --from=build /out/vbmd /usr/local/bin/vbmd

USER vbm:vbm
WORKDIR /home/vbm

ENTRYPOINT ["/usr/local/bin/vbmd"]
CMD ["server"]
