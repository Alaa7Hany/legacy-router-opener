#!/usr/bin/env bash
# Verify a desktop binary reports the expected version.
# Shared by the Linux and macOS jobs in ci.yml and release.yml.
# Usage: ./verify-version.sh <binary-filename> <expected-version>
set -euo pipefail

cd "$(dirname "$0")"

BINARY="$1"
EXPECTED="$2"

if [[ ! -x "$BINARY" ]]; then
    echo "Binary '$BINARY' not found or not executable in $(pwd)." >&2
    exit 1
fi

output="$(./"$BINARY" --version)"
echo "Version output: $output"
if [[ "$output" != "legacy-router-opener $EXPECTED" ]]; then
    echo "Unexpected --version output: '$output' (expected 'legacy-router-opener $EXPECTED')" >&2
    exit 1
fi
