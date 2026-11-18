# Design & Platform Decisions

Key product and engineering choices made for the Desktop version, and why they exist.

---

## 1. Portable Windows Executable

- **Original approach:** The first desktop version was a Node.js script, now kept at [`desktop/legacy/index.mjs`](../legacy/index.mjs). It required users to install Node.js v18+ first or download an ~80 MB bundled runtime.
- **Problem:** When a router is reset or misconfigured, the user often has **no internet connection** to download Node.js, and everyday users do not want to install developer runtimes just to log into their Wi-Fi router.
- **Decision:** The Go app bundles the browser UI into a single executable that can run offline. Double-clicking it opens the browser without a terminal. If startup fails, the Windows build shows an error dialog. Developers can use `go run .` for terminal logs or run the Node.js fallback.

---

## 2. Single-Action Opener Card Instead of a Full Monitoring Dashboard

- **Problem:** Early drafts placed IP editors, device profiles, live TLS telemetry grids, and scrolling logs on the main screen at equal visual weight. For someone who just wants to open `192.168.1.1` and change a Wi-Fi password, that felt cluttered and technical.
- **Decision:** The opener starts with one primary button, **Open router page**. **Fix stuck login** sits beneath it, and changing devices or viewing connection details takes one click to expand. The page starts in the browser’s preferred supported language and remembers a manual English or Arabic choice for that browser and local address.

---

## 3. Neutral Light/Dark Classic Theme

- **Decision:** The interface automatically follows your operating system's light or dark preference using neutral warm-stone / matte-gray surfaces and high-contrast text, with plain `[GET]` / `[FWD]` / `[200]` / `[ERR]` tags in the hidden log drawer rather than decorative status badges.

---

## 4. Separating `/_bridge` from `/`

- **Why not put the opener controls at `/` and proxy the router under `/router/`?**
  Older routers, switches, and server controllers hardcode root-relative links like `/index/login.cgi`, `/js/util.js`, and `/images/logo.gif`, and rely heavily on `<frameset>` layouts. Moving the device off `/` breaks scripts and images across dozens of hardware models.
- **Decision:** The proxied device is served at `/` and the opener lives at `/_bridge`. **Quit app** opens a confirmation on a separate local browser origin so scripts from the proxied device cannot shut down the app.

---

## 5. Remembering Legacy Fallback Per Device

- **Problem:** Some routers reject modern TLS 1.3 handshake extensions on port 443 and require an older TLS 1.0–1.2 handshake. Trying the modern handshake on every single image or script adds unnecessary delay.
- **Decision:** Once a device falls back to the older TLS profile (or plain HTTP on port 80), the desktop bridge remembers that preference for that address in memory so subsequent requests load immediately. Clicking **Fix stuck login** clears those remembered states if you switch or reboot hardware.
