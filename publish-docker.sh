#!/usr/bin/env bash
# Build and publish the vbmd Docker image to Docker Hub.
#
# Usage:
#   DOCKERHUB_USER=<user> ./publish-docker.sh [tag]
#
# Required env:
#   DOCKERHUB_USER    Docker Hub username (image will be pushed as
#                     <user>/<IMAGE_NAME>:<tag>).
#
# Optional env:
#   DOCKERHUB_TOKEN   Docker Hub access token. If set, login is non-
#                     interactive. If unset, falls back to `docker login`
#                     prompt.
#   IMAGE_NAME        Image name (default: vbmd).
#   TAG               Tag to publish (default: positional arg, else short
#                     git SHA, else "latest").
#   ALLOW_DIRTY=1     Permit publishing from a working tree with uncommitted
#                     changes. Off by default — uncommitted state usually
#                     means the image is not reproducible.
#   SKIP_LATEST=1     Don't also tag/push :latest.
#
# Exits non-zero on any failure (set -euo pipefail). Tests run inside the
# build via the Dockerfile, so a failing test fails the publish.

set -euo pipefail

cd "$(dirname "$0")"

: "${DOCKERHUB_USER:?DOCKERHUB_USER must be set (e.g. DOCKERHUB_USER=alice ./publish-docker.sh)}"
IMAGE_NAME="${IMAGE_NAME:-vbmd}"

# Resolve tag: explicit arg > $TAG env > git short SHA > "latest".
if [[ $# -ge 1 ]]; then
    TAG="$1"
elif [[ -n "${TAG:-}" ]]; then
    TAG="${TAG}"
elif sha=$(git rev-parse --short HEAD 2>/dev/null); then
    TAG="$sha"
else
    TAG="latest"
fi

# Refuse dirty trees unless explicitly overridden.
if [[ "${ALLOW_DIRTY:-0}" != "1" ]]; then
    if ! git diff --quiet || ! git diff --cached --quiet; then
        echo "ERROR: working tree has uncommitted changes." >&2
        echo "       Commit, stash, or set ALLOW_DIRTY=1 to override." >&2
        exit 1
    fi
fi

REMOTE="docker.io/${DOCKERHUB_USER}/${IMAGE_NAME}"
LOCAL="${IMAGE_NAME}:${TAG}"

echo "==> Logging in to Docker Hub as ${DOCKERHUB_USER}"
if [[ -n "${DOCKERHUB_TOKEN:-}" ]]; then
    echo "${DOCKERHUB_TOKEN}" \
        | docker login docker.io --username "${DOCKERHUB_USER}" --password-stdin
else
    docker login docker.io --username "${DOCKERHUB_USER}"
fi

# Logout on exit so credentials don't linger on shared CI machines.
trap 'docker logout docker.io >/dev/null 2>&1 || true' EXIT

echo "==> Building ${LOCAL} (tests run inside build stage)"
docker build -t "${LOCAL}" daemon/

echo "==> Tagging ${REMOTE}:${TAG}"
docker tag "${LOCAL}" "${REMOTE}:${TAG}"

echo "==> Pushing ${REMOTE}:${TAG}"
docker push "${REMOTE}:${TAG}"

if [[ "${SKIP_LATEST:-0}" != "1" ]]; then
    echo "==> Tagging + pushing ${REMOTE}:latest"
    docker tag "${LOCAL}" "${REMOTE}:latest"
    docker push "${REMOTE}:latest"
fi

echo "==> Done. Published:"
echo "    ${REMOTE}:${TAG}"
[[ "${SKIP_LATEST:-0}" != "1" ]] && echo "    ${REMOTE}:latest"
