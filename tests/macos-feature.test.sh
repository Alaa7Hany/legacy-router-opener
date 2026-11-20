#!/usr/bin/env bash
# Characterization tests for the macOS desktop feature.
# Seams: version helper, build script CLI, version verifier, workflow config, README contract.
# Usage: bash tests/macos-feature.test.sh [all|version|build|verify|workflows|docs]
# Set ROOT to test a different checkout (used by the red-check mutations).
set -uo pipefail

ROOT="${ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
ONLY="${1:-all}"
PASSED=0
FAILED=0

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

report() {
  if "$@"; then
    printf '  PASS  %s\n' "$TEST_NAME"
    PASSED=$((PASSED + 1))
  else
    printf '  FAIL  %s\n' "$TEST_NAME"
    FAILED=$((FAILED + 1))
  fi
}

eq() { [ "$1" = "$2" ] || { printf 'expected "%s", got "%s"\n' "$2" "$1" >&2; return 1; }; }
contains() { case "$1" in *"$2"*) return 0 ;; *) printf '"%s" does not contain "%s"\n' "$1" "$2" >&2; return 1 ;; esac; }
must_fail() { if "$@" >/dev/null 2>&1; then printf 'command unexpectedly succeeded\n' >&2; return 1; fi; return 0; }

normalize() { bash -c 'source "$1"; normalize_version "$2"' _ "$ROOT/desktop/_version.sh" "$1"; }

# --- Seam 1: version normalization -------------------------------------------
seam_version() {
  printf 'Version helper (%s)\n' "$ROOT"
  local out
  TEST_NAME="version: plain semver preserved"; out="$(normalize '1.2.3')"; report eq "$out" '1.2.3'
  TEST_NAME="version: whitespace and leading v stripped"; out="$(normalize '  v1.2.3  ')"; report eq "$out" '1.2.3'
  TEST_NAME="version: leading V stripped"; out="$(normalize 'V2.0')"; report eq "$out" '2.0'
  TEST_NAME="version: prerelease suffix preserved"; out="$(normalize 'v1.2.3-rc.1')"; report eq "$out" '1.2.3-rc.1'
  TEST_NAME="version: five segments rejected"; report must_fail normalize '1.2.3.4.5'
  TEST_NAME="version: non-numeric segment rejected"; report must_fail normalize '1.2.bad'
  TEST_NAME="version: segment above 65535 rejected"; report must_fail normalize '99999.1'
  TEST_NAME="version: empty input rejected"; report must_fail normalize ''
}

# --- Seam 2: build script CLI -------------------------------------------------
setup_build_copy() {
  cp -a "$ROOT/desktop" "$work/desktop"
  rm -f "$work/desktop"/legacy-router-opener "$work/desktop"/legacy-router-opener.exe \
        "$work/desktop"/legacy-router-opener-macos-arm64 "$work/desktop"/legacy-router-opener-macos-amd64
}

seam_build() {
  printf 'Build script CLI (%s)\n' "$ROOT"
  setup_build_copy
  local d="$work/desktop"

  TEST_NAME="build: arm64 artifact is Mach-O arm64"
  report bash -c 'cd "$1" && GOTOOLCHAIN=auto ./build-macos.sh 0.0.0-test arm64 >/dev/null && file legacy-router-opener-macos-arm64 | grep -q "Mach-O 64-bit arm64"' _ "$d"

  TEST_NAME="build: amd64 artifact is Mach-O x86_64"
  report bash -c 'cd "$1" && GOTOOLCHAIN=auto ./build-macos.sh 0.0.0-test amd64 >/dev/null && file legacy-router-opener-macos-amd64 | grep -q "Mach-O 64-bit x86_64"' _ "$d"

  TEST_NAME="build: version is stamped into the arm64 binary"
  report grep -a -F -q '0.0.0-test' "$d/legacy-router-opener-macos-arm64"

  TEST_NAME="build: unsupported architecture is rejected"
  report must_fail bash -c 'cd "$1" && ./build-macos.sh 1.0.0 ppc64le' _ "$d"
}

# --- Seam 3: version verifier -------------------------------------------------
seam_verify() {
  printf 'Version verifier (%s)\n' "$ROOT"
  setup_build_copy
  local d="$work/desktop"
  (cd "$d" && GOTOOLCHAIN=auto ./build.sh 0.0.0-test >/dev/null) || { TEST_NAME="verify: prerequisite Linux build"; report false; return 0; }

  TEST_NAME="verify: matching version exits 0"
  report bash -c 'cd "$1" && ./verify-version.sh legacy-router-opener 0.0.0-test >/dev/null' _ "$d"

  TEST_NAME="verify: wrong expected version exits non-zero"
  report must_fail bash -c 'cd "$1" && ./verify-version.sh legacy-router-opener 9.9.9' _ "$d"

  TEST_NAME="verify: missing binary exits non-zero"
  report must_fail bash -c 'cd "$1" && ./verify-version.sh no-such-binary 0.0.0-test' _ "$d"
}

# --- Seam 4: workflow config --------------------------------------------------
seam_workflows() {
  printf 'Workflow config (%s)\n' "$ROOT"
  local result
  result="$(python3 - "$ROOT" <<'PY'
import sys, yaml
root = sys.argv[1]
ci = yaml.safe_load(open(f"{root}/.github/workflows/ci.yml"))
rel = yaml.safe_load(open(f"{root}/.github/workflows/release.yml"))
checks = []
def ck(name, cond): checks.append((name, bool(cond)))
ck("ci: desktop-macos job exists", "desktop-macos" in ci["jobs"])
job = ci["jobs"].get("desktop-macos", {})
ck("ci: matrix is arm64+amd64", job.get("strategy", {}).get("matrix", {}).get("arch") == ["arm64", "amd64"])
ck("ci: runner expression selects Intel runner for amd64", "macos-15-intel" in str(job.get("runs-on")) and "macos-latest" in str(job.get("runs-on")))
ck("ci: uses shared verify-version.sh", any("verify-version.sh" in str(s.get("run","")) for s in job.get("steps", [])))
rjob = rel["jobs"].get("build-macos", {})
ck("release: build-macos job exists", bool(rjob))
ck("release: same matrix", rjob.get("strategy", {}).get("matrix", {}).get("arch") == ["arm64", "amd64"])
ck("release: runner expression present", "macos-15-intel" in str(rjob.get("runs-on")))
ck("release: publish depends on build-macos", "build-macos" in rel["jobs"]["publish-release"]["needs"])
text = open(f"{root}/.github/workflows/release.yml").read()
ck("release: checksums include both macOS assets", "legacy-router-opener-macos-arm64" in text and "legacy-router-opener-macos-amd64" in text and "SHA256SUMS.txt" in text)
ck("release: uploads include both macOS assets", text.count("legacy-router-opener-macos-arm64") >= 3 and text.count("legacy-router-opener-macos-amd64") >= 3)
for name, ok in checks:
    print(("PASS  " if ok else "FAIL  ") + name)
sys.exit(0 if all(ok for _, ok in checks) else 1)
PY
)"
  printf '%s\n' "$result"
  local line name ok
  while IFS= read -r line; do
    name="${line#*  }"; ok="${line%%  *}"
    TEST_NAME="$name"
    if [ "$ok" = "PASS" ]; then PASSED=$((PASSED + 1)); else FAILED=$((FAILED + 1)); fi
  done <<< "$result"
  case "$result" in *FAIL*) return 1 ;; esac
  return 0
}

# --- Seam 5: README contract --------------------------------------------------
seam_docs() {
  printf 'README contract (%s)\n' "$ROOT"
  local r="$ROOT/README.md"
  TEST_NAME="docs: macOS Quick Start section"; report grep -q '^### macOS (Apple Silicon / Intel)' "$r"
  TEST_NAME="docs: new macOS link uses AlaaHany"; report grep -q 'https://github.com/AlaaHany/legacy-router-opener/releases' "$r"
  TEST_NAME="docs: shared Run the bridge section exists"; report grep -q '^### Run the bridge (Windows, Linux, macOS)' "$r"
  TEST_NAME="docs: bridge steps are not duplicated"; report bash -c '[ "$(grep -c "Your browser opens" "$1")" -eq 1 ]' _ "$r"
  TEST_NAME="docs: tagline lists macOS"; report grep -q 'Windows, Linux, macOS, and Android' "$r"
}

case "$ONLY" in
  all)      seam_version; seam_build; seam_verify; seam_workflows; seam_docs ;;
  version)  seam_version ;;
  build)    seam_build ;;
  verify)   seam_verify ;;
  workflows) seam_workflows ;;
  docs)     seam_docs ;;
  *) printf 'unknown seam: %s\n' "$ONLY" >&2; exit 2 ;;
esac

printf '\n%d passed, %d failed\n' "$PASSED" "$FAILED"
[ "$FAILED" -eq 0 ]
