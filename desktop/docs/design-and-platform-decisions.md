# Design & Platform Decisions

Key product and engineering choices made for the Desktop version, and why they exist.

---

## 1. Standalone Go Executable (~7.6 MB) Instead of Bundled Node.js (~80 MB)

- **Original approach:** The first desktop version was a Node.js script (`index.mjs`), which required users to install Node.js v18+ first or download an ~80 MB bundled runtime.
- **Problem:** When a router is reset or misconfigured, the user often has **no internet connection** to download Node.js, and everyday users do not want to install developer runtimes just to log into their Wi-Fi router.
- **Decision:** We rebuilt the desktop proxy in Go with zero third-party packages. It compiles into a single ~7.6 MB file (`legacy-router-opener.exe`) that runs immediately offline with nothing to install. *(The lightweight `index.mjs` script remains in the `desktop/` folder as a developer fallback.)*

---

## 2. Single-Action Opener Card Instead of a Full Monitoring Dashboard

- **Problem:** Early drafts placed IP editors, device profiles, live TLS telemetry grids, and scrolling logs on the main screen at equal visual weight. For someone who just wants to open `192.168.1.1` and change a Wi-Fi password, that felt cluttered and technical.
- **Decision:** The launchpad opens directly to one small, centered card with a single clear primary button (**Open Router Page (`192.168.1.1`)**) and a plain-English **Reset session** link underneath. Power-user options (changing the IP/port, picking a device compatibility profile, and viewing connection logs) stay neatly tucked inside collapsible sections until needed.

---

## 3. Neutral Light/Dark Classic Theme

- **Decision:** The interface automatically follows your operating system's light or dark preference using neutral warm-stone / matte-gray surfaces and high-contrast text, with plain `[GET]` / `[FWD]` / `[200]` / `[ERR]` tags in the hidden log drawer rather than decorative status badges.

---

## 4. Separating `/_bridge` from `/`

- **Why not put the opener controls at `/` and proxy the router under `/router/`?**
  Older routers, switches, and server controllers hardcode root-relative links like `/index/login.cgi`, `/js/util.js`, and `/images/logo.gif`, and rely heavily on `<frameset>` layouts. Moving the device off `/` breaks scripts and images across dozens of hardware models.
- **Decision:** The proxied device owns `/` exclusively, while the opener controls live safely at `/_bridge` (which no router or embedded device uses).

---

## 5. Remembering Legacy Fallback Per Device

- **Problem:** Some routers reject modern TLS 1.3 handshake extensions on port 443 and require an older TLS 1.0–1.2 handshake. Trying the modern handshake on every single image or script adds unnecessary delay.
- **Decision:** Once a device falls back to the older TLS profile (or plain HTTP on port 80), the desktop bridge remembers that preference for that address in memory so subsequent requests load immediately. Clicking **Reset session** clears those remembered states if you switch or reboot hardware.
