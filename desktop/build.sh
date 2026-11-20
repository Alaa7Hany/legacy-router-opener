#!/usr/bin/env bash
# Build the Legacy Router Opener desktop binary for Linux.
#
# Usage:
#   ./build.sh [VERSION]           # e.g. ./build.sh 1.0.0 (a leading "v" is stripped)
#   GOARCH=arm64 ./build.sh 1.0.0  # build for linux/arm64 instead of amd64
#
# For Windows builds use build.ps1 (adds go-winres icon/version resources and
# the windowsgui flag). Keep version handling in sync with build.ps1 and the
# Makefile: surrounding whitespace is trimmed, an optional leading "v"/"V" is
# dropped, and each dot-separated segment must be a number from 0 to 65535.
#
# Defaults to a development version when no argument is provided.
set -euo pipefail

cd "$(dirname "$0")"

VERSION="${1-0.0.0-dev}"

# Trim surrounding whitespace (mirrors build.ps1's $Version.Trim()).
TRIMMED_VERSION="${VERSION#"${VERSION%%[![:space:]]*}"}"
TRIMMED_VERSION="${TRIMMED_VERSION%"${TRIMMED_VERSION##*[![:space:]]}"}"

# Drop an optional leading "v"/"V" so the embedded version never carries it.
TRIMMED_VERSION="${TRIMMED_VERSION#v}"
TRIMMED_VERSION="${TRIMMED_VERSION#V}"

if [[ -z "$TRIMMED_VERSION" ]]; then
    echo "Version cannot be empty." >&2
    exit 1
fi

CORE_VERSION="${TRIMMED_VERSION%%-*}"
CORE_VERSION="${CORE_VERSION%%+*}"
IFS='.' read -r -a SEGMENTS <<< "$CORE_VERSION"
if [[ "${#SEGMENTS[@]}" -lt 1 || "${#SEGMENTS[@]}" -gt 4 ]]; then
    echo "Cannot derive version segments from '$VERSION'." >&2
    exit 1
fi
for segment in "${SEGMENTS[@]}"; do
    if [[ ! "$segment" =~ ^[0-9]{1,5}$ ]] || (( 10#$segment > 65535 )); then
        echo "Invalid numeric version segment '$segment' in '$VERSION'." >&2
        exit 1
    fi
done

GOOS="linux"
GOARCH="${GOARCH:-amd64}"
GOTOOLCHAIN="${GOTOOLCHAIN:-auto}"
OUTPUT="legacy-router-opener"

LDFLAGS="-s -w -X main.version=${TRIMMED_VERSION}"

echo "Building ${OUTPUT} (${GOOS}/${GOARCH}) version ${TRIMMED_VERSION}..."
GOOS="$GOOS" GOARCH="$GOARCH" GOTOOLCHAIN="$GOTOOLCHAIN" go build -trimpath -ldflags="$LDFLAGS" -o "$OUTPUT" .

echo "Built: $(pwd)/${OUTPUT}"
