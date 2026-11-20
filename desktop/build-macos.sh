#!/usr/bin/env bash
# Build the Legacy Router Opener desktop binary for macOS.
#
# Usage:
#   ./build-macos.sh [VERSION] [ARCH]   # e.g. ./build-macos.sh 1.0.0 arm64
#                                       # ARCH must be arm64 (default) or amd64
#
# Version normalization/validation lives in _version.sh, shared with build.sh.
# Keep build.ps1's equivalent logic in sync.
#
# The Go source already supports macOS at runtime (browser launch via `open`,
# default gateway detection via `route -n get default`), so no source changes are
# needed. The binary is unsigned by design; see the macOS Quick Start in the
# project README for the one-time quarantine-removal step.
set -euo pipefail

cd "$(dirname "$0")"
source ./_version.sh

VERSION="${1-0.0.0-dev}"
ARCH="${2:-arm64}"
TRIMMED_VERSION="$(normalize_version "$VERSION")"

case "$ARCH" in
    arm64|amd64) ;;
    *)
        echo "ARCH must be 'arm64' or 'amd64', got '$ARCH'." >&2
        exit 1
        ;;
esac

GOOS="darwin"
GOTOOLCHAIN="${GOTOOLCHAIN:-auto}"
OUTPUT="legacy-router-opener-macos-${ARCH}"

LDFLAGS="-s -w -X main.version=${TRIMMED_VERSION}"

echo "Building ${OUTPUT} (${GOOS}/${ARCH}) version ${TRIMMED_VERSION}..."
GOOS="$GOOS" GOARCH="$ARCH" GOTOOLCHAIN="$GOTOOLCHAIN" go build -trimpath -ldflags="$LDFLAGS" -o "$OUTPUT" .

echo "Built: $(pwd)/${OUTPUT}"
