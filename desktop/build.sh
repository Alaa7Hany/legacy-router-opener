#!/usr/bin/env bash
# Build the Legacy Router Opener desktop binary for Linux.
#
# Usage:
#   ./build.sh [VERSION]
#
# Defaults to a development version when no argument is provided.
set -euo pipefail

cd "$(dirname "$0")"

VERSION="${1:-0.0.0-dev}"
TRIMMED_VERSION="${VERSION#v}"
TRIMMED_VERSION="${TRIMMED_VERSION#V}"

if [[ -z "$TRIMMED_VERSION" ]]; then
    echo "Version cannot be empty." >&2
    exit 1
fi

GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"

OUTPUT="legacy-router-opener"
if [[ "$GOOS" == "windows" ]]; then
    OUTPUT="${OUTPUT}.exe"
fi

LDFLAGS="-s -w -X main.version=${TRIMMED_VERSION}"

echo "Building ${OUTPUT} (${GOOS}/${GOARCH}) version ${TRIMMED_VERSION}..."
GOOS="$GOOS" GOARCH="$GOARCH" GOTOOLCHAIN=auto go build -trimpath -ldflags="$LDFLAGS" -o "$OUTPUT" .

echo "Built: $(pwd)/${OUTPUT}"
