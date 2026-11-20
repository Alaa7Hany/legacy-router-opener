#!/usr/bin/env bash
# Build the Legacy Router Opener desktop binary for Linux.
#
# Usage:
#   ./build.sh [VERSION]           # e.g. ./build.sh 1.0.0 (a leading "v" is stripped)
#   GOARCH=arm64 ./build.sh 1.0.0  # build for linux/arm64 instead of amd64
#
# Version normalization/validation lives in _version.sh, shared with
# build-macos.sh. For Windows builds use build.ps1 (adds go-winres icon/version
# resources and the windowsgui flag); keep its equivalent logic in sync.
#
# Defaults to a development version when no argument is provided.
set -euo pipefail

cd "$(dirname "$0")"
source ./_version.sh

VERSION="${1-0.0.0-dev}"
TRIMMED_VERSION="$(normalize_version "$VERSION")"

GOOS="linux"
GOARCH="${GOARCH:-amd64}"
GOTOOLCHAIN="${GOTOOLCHAIN:-auto}"
OUTPUT="legacy-router-opener"

LDFLAGS="-s -w -X main.version=${TRIMMED_VERSION}"

echo "Building ${OUTPUT} (${GOOS}/${GOARCH}) version ${TRIMMED_VERSION}..."
GOOS="$GOOS" GOARCH="$GOARCH" GOTOOLCHAIN="$GOTOOLCHAIN" go build -trimpath -ldflags="$LDFLAGS" -o "$OUTPUT" .

echo "Built: $(pwd)/${OUTPUT}"
