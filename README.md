<p align="center">
  <img src="desktop/assets/icon.png" alt="Legacy Device &amp; Router Opener icon" width="64" height="64">
</p>

<h1 align="center">Legacy Device &amp; Router Opener</h1>

<p align="center">
  Zero-dependency, offline local TLS bridge for Windows, Linux, macOS, and Android for opening older routers, printers, switches, and server management consoles in modern browsers without Docker, Windows 7 VMs, or unpatched browsers.
</p>

<p align="center">
  <a href="https://3laa.me/legacy-router-opener/">Website (English)</a> · <a href="https://3laa.me/legacy-router-opener/ar/">الموقع (العربية)</a>
</p>

<table align="center">
  <tr>
    <td align="center"><img src="docs/screenshots/opener-en-light.png" alt="Legacy Device and Router Opener — English Light Mode" width="360"></td>
    <td align="center"><img src="docs/screenshots/opener-ar-dark.png" alt="Legacy Device and Router Opener — Arabic RTL Dark Mode" width="360"></td>
  </tr>
  <tr>
    <td align="center"><sub>English (Light)</sub></td>
    <td align="center"><sub>Arabic RTL (Dark)</sub></td>
  </tr>
</table>

## Quick Start

### Windows

1. Download `legacy-router-opener.exe` from [GitHub Releases](https://github.com/Alaa7Hany/legacy-router-opener/releases) and double-click it — no install or runtime needed.
2. Follow **Run the bridge** below.

> **Windows SmartScreen:** Release binaries are currently unsigned — click **More info** → **Run anyway**.
> Releases are built cleanly by GitHub Actions with `SHA256SUMS.txt` (`Get-FileHash .\legacy-router-opener.exe -Algorithm SHA256`).

### Linux

1. Download `legacy-router-opener` from [GitHub Releases](https://github.com/Alaa7Hany/legacy-router-opener/releases) and run it from a terminal — no install or runtime needed:
   ```bash
   sha256sum --ignore-missing -c SHA256SUMS.txt   # verify the download
   chmod +x legacy-router-opener                   # only if your browser stripped the executable bit
   ./legacy-router-opener
   ```
2. Follow **Run the bridge** below.

### macOS (Apple Silicon / Intel)

1. Download the build for your Mac from [GitHub Releases](https://github.com/AlaaHany/legacy-router-opener/releases): `legacy-router-opener-macos-arm64` for Apple Silicon, or `legacy-router-opener-macos-amd64` for Intel. Then run it from a terminal — no install or runtime needed:
   ```bash
   # Verify: compare this hash with the matching line in SHA256SUMS.txt
   shasum -a 256 legacy-router-opener-macos-arm64   # or -amd64
   chmod +x legacy-router-opener-macos-arm64        # or -amd64
   ./legacy-router-opener-macos-arm64               # or -amd64
   ```
   Compare the hash with the matching line for your asset in `SHA256SUMS.txt`.
2. Follow **Run the bridge** below.

> **macOS Gatekeeper:** Release binaries are currently unsigned. If macOS says the app "cannot be opened because the developer cannot be verified", either right-click the file → **Open**, or run `xattr -d com.apple.quarantine legacy-router-opener-macos-arm64` (use `-amd64` for Intel) once before launching.

### Run the bridge (Windows, Linux, macOS)

1. Your browser opens `http://localhost:8180/_bridge` (with built-in **English & العربية RTL** toggle).
2. Click **Open router page** (`http://localhost:8080/`), or expand **Change device** to pick a target IP or device profile.
3. Keep the opener tab open while using your device; closing the last opener tab stops the bridge after 4 seconds (or click **Quit app**).

### Android (Android 8.0+ / API 26+)

1. Download `legacy-router-opener.apk` from [GitHub Releases](https://github.com/Alaa7Hany/legacy-router-opener/releases) and install it (or run `adb install legacy-router-opener.apk`).
2. Tap **Open router page** to use the built-in viewer, or **Open in external browser** (Chrome, Edge, Firefox, Samsung Internet) — includes a built-in **English & العربية RTL** toggle.
3. Expand **Change device** to pick a target IP or device profile; tap **Stop local proxy** (in-app or from the notification) when done.

> **APK Verification:** Verify downloads against `SHA256SUMS.txt` and the official APK signing certificate SHA-256 fingerprint:
> `28c3e864cf3aa9005bbe38bff68a2b152e256d18940e2189d7bc7987b5a26fb4`

## What It Fixes

Resolves `ERR_SSL_VERSION_OR_CIPHER_MISMATCH`, `SSL_ERROR_UNSUPPORTED_VERSION`, `ERR_SSL_UNSAFE_LEGACY_RENEGOTIATION_DISABLED`, `NET::ERR_CERT_AUTHORITY_INVALID`, `ERR_SSL_WEAK_SERVER_EPHEMERAL_DH_KEY`, and `ERR_CONNECTION_CLOSED`:

- **Routers & Access Points:** Older ISP and home routers — Huawei (`HG531`, `HG532n`, `HG630`, `DG8045`), ZTE (`ZXHN H108N`, `H168N`), D-Link, Netgear, TP-Link, and repurposed secondary Wi-Fi routers
- **Server Consoles:** HPE iLO 2 / 3 / 4 and Dell iDRAC 6 / 7
- **Switches & UPS:** Cisco / HP switches and firewalls, APC UPS (`NMC1` / `NMC2`)
- **Printers, NAS & Cameras:** HP LaserJet / Officejet EWS, QNAP / ReadyNAS, Hikvision / Dahua cameras

*Protocol coverage:*
- **Windows, Linux & macOS (`.exe` / desktop binary):** TLS 1.0–1.3, 1024-bit RSA/SHA-1, `RC4`, `3DES`, and unsafe legacy renegotiation. Go's stdlib (`crypto/tls`) does not implement finite-field `DHE-RSA` or `SSLv3` — use plain HTTP (`:80`), [`desktop/legacy/index.mjs`](desktop/legacy/index.mjs), or the **Android app** for those devices.
- **Android (`.apk`):** Bundled Bouncy Castle legacy TLS engine covering TLS 1.0–1.3, `SSLv3`, `512-bit`/`768-bit DHE-RSA` (`DH_KEY_TOO_SMALL`), `RC4`/`3DES`, and pre-RFC-5746 unsafe renegotiation — no root, no VPN, and no browser setting changes.

See [Troubleshooting & Protocol Matrix](desktop/docs/troubleshooting-and-device-modes.md) for the full matrix.

## Security at a Glance

- **Loopback-only:** Listens exclusively on loopback (`127.0.0.1` / `[::1]` on the desktop app — Windows, Linux and macOS; `127.0.0.1` on Android).
- **Origin isolation:** The desktop app isolates control/quit endpoints (`:8180`) from the proxied device (`:8080`) with strict `Host`, `Origin`, CSRF, and CSP enforcement; Android issues a fresh random `<hex>.localhost` origin per session and clears retired cookies/storage on switch or reset.
- **Offline & private:** Zero telemetry, zero analytics, zero ads, and no modifications to system or browser settings.

## Build & Docs

### Windows
```powershell
cd desktop
go test ./...
.\build.ps1 -Version 1.0.0
```

### Linux
```bash
cd desktop
go test ./...
./build.sh 1.0.0   # or: make build VERSION=1.0.0
```

### macOS
```bash
cd desktop
go test ./...
./build-macos.sh 1.0.0 arm64   # or: make build-macos VERSION=1.0.0
./build-macos.sh 1.0.0 amd64   # or: make build-macos VERSION=1.0.0 MACOS_ARCH=amd64
```
Cross-compiles from any host — Go's stdlib-only macOS runtime support needs no source changes. The binary is unsigned; see the macOS Quick Start for the quarantine-removal step.

### Android
```bash
cd android
./gradlew testDebugUnitTest assembleRelease
```

### Makefile (desktop)
The desktop module ships a `Makefile` as a thin wrapper over `build.sh`, `build.ps1` and `build-macos.sh`:
```bash
cd desktop
make                              # build for Linux (amd64 by default; GOARCH=arm64 to override)
make test                         # run Go tests
make run                          # run with --no-browser
make build VERSION=1.0.0          # Linux build with a version stamp
make build-windows VERSION=1.0.0  # Windows .exe via build.ps1 (requires pwsh)
make build-macos VERSION=1.0.0    # macOS/arm64 via build-macos.sh
make clean
```

### Hot reload (development)
Install [Air](https://github.com/air-verse/air) and run from `desktop/`:
```bash
go install github.com/air-verse/air@latest
air
```
`desktop/.air.toml` watches the Go sources and the embedded HTML/CSS/JS assets, rebuilds on change, and restarts the binary from `desktop/tmp/` with `--no-browser` — open the control page at `http://localhost:8180/_bridge` yourself. Build output in `desktop/tmp/` is gitignored and cleared when Air exits.

### Docs
- **Project:** [Glossary](GLOSSARY.md) · [Architecture Decisions](docs/adr/)
- **Desktop Docs:** [How It Works](desktop/docs/how-it-works.md) · [Design Decisions](desktop/docs/design-and-platform-decisions.md) · [Troubleshooting & CLI Flags](desktop/docs/troubleshooting-and-device-modes.md)
- **Android Docs:** [Overview](android/docs/README.md) · [How It Works](android/docs/how-it-works.md) · [Design Decisions](android/docs/design-and-platform-decisions.md) · [Troubleshooting](android/docs/troubleshooting.md)

## Support & License

Optional support: [Ko-fi](https://ko-fi.com/lol7harrr) · [GitHub Sponsors](https://github.com/sponsors/Alaa7Hany)

Licensed under **GNU GPLv3** — see [LICENSE](LICENSE).
