---
status: accepted
---

# macOS Desktop Release

The desktop Go app already supports macOS at runtime (browser launch via `open`, default gateway detection via `route -n get default`), but had no macOS build, CI job, or release asset. We ship unsigned per-architecture macOS binaries — `legacy-router-opener-macos-arm64` and `legacy-router-opener-macos-amd64` — as GitHub Release assets, built with a dedicated `build-macos.sh` and verified by CI, with no changes to the Go source. This extends the existing Windows/Linux release pattern without introducing a second distribution model.

## Considered Options

- **Signed and notarized binary**: rejected — requires a paid Apple Developer account and notarization pipeline for a tool whose Windows counterpart is also unsigned.
- **`.app` bundle or Homebrew formula**: rejected — the app is a CLI that opens the default browser; a plain binary matches how Windows and Linux are shipped.
- **Universal binary via `lipo`**: rejected — needs macOS-only tooling and adds a merge step; two explicit architecture assets are simpler to build and verify in CI.

## Consequences

- macOS users must clear Gatekeeper's quarantine once before first launch (right-click → Open, or `xattr -d com.apple.quarantine …`); this is documented in the README Quick Start.
- Two release assets and two checksum entries are published instead of one.
- No macOS machine was available to the maintainer at decision time, so verification is CI-only: `go vet`, unit tests, build, and `--version` smoke checks on `macos-latest` for both architectures. Browser launch, loopback binding on a real Mac, and proxying an actual legacy device are unverified and deferred to users.
- The Go source stays unchanged; future macOS behaviour changes remain in the shared, OS-agnostic code paths.
