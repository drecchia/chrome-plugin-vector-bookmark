# syntax=docker/dockerfile:1.7

# ── build stage ───────────────────────────────────────────────────────────────
FROM golang:1.22-bookworm AS build

WORKDIR /src

# Cache go.mod/sum first so dep download is reused across rebuilds.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Pure-Go build (modernc.org/sqlite is CGO-free). Static binary so it lands
# cleanly in distroless/static.
ENV CGO_ENABLED=0 \
    GOOS=linux

RUN go build -trimpath -ldflags="-s -w" -o /out/vbmd ./cmd/vbmd

# ── runtime stage ─────────────────────────────────────────────────────────────
# distroless/static is ~2MB, no shell, no package manager. The :nonroot tag
# resolves to UID 65532. Use VBM_DATA_DIR=/data so the daemon doesn't try to
# resolve $HOME (which is unset in distroless).
FROM gcr.io/distroless/static-debian12:nonroot

VOLUME ["/data"]

EXPOSE 7532

COPY --from=build /out/vbmd /vbmd

USER 65532:65532

ENTRYPOINT ["/vbmd"]
CMD ["server"]
