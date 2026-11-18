# Troubleshooting & Device Profiles

Quick answers when a router or legacy device refuses to log in or load properly.

---

## "Duplicated Login" or Stuck Login Screen

Older Huawei/ZTE routers and HPE iLO controllers only allow **one active admin session** at a time, and some devices keep stale cookies stuck in your browser.

**How to fix:**
1. Go back to the **Router Page Opener** tab. If you closed it, double-click `legacy-router-opener.exe` to reopen it.
2. Click **Fix stuck login** directly beneath the main button.
3. This clears the bridge's cookie jar, tells your browser to drop stale `localhost` cookies, and flushes cached pages.
4. Click **Open router page** again.

> **Tip:** If you use another local tool that also connects to your router's admin account at the same time, disconnect that tool first so the router doesn't kick you out with a duplicate-login error.

---

## Opening a Device That Isn't at `192.168.1.1`

The app auto-detects your computer's default gateway on startup. If you want to open another device (such as a secondary router on `192.168.0.1`, a switch, a network printer, or a server controller):

1. Click **Change device** on the opener card to expand the options.
2. Enter the address in **Device address**:
   - Plain IP (tries HTTPS 443, falls back to HTTP 80): `192.168.0.1`
   - Explicit plain HTTP: `http://192.168.1.254`
   - Custom port: `192.168.0.120:8443`
3. Click **Apply changes** or click the main **Open router page** button.
4. To fill in your main Wi-Fi router’s address, click **Use detected address** above the address box.

---

## Device Compatibility Profiles

Expand **Change device** and open **Device type** if a specific piece of hardware is picky:

| Profile | Best For | What It Adjusts |
| :--- | :--- | :--- |
| **Automatic (recommended)** | Most home routers and unknown devices | Starts with balanced defaults and automatically adapts to HTTP or legacy TLS if needed. |
| **Huawei / ZTE router** | Older ISP DSL/VDSL routers (`HG531`, `HG532n`, `H108N`, `H168N`) | Uses legacy TLS immediately and pre-seeds required menu/language cookies. |
| **Dell iDRAC 6 / 7** | Older PowerEdge server management | Rewrites internal hostname redirects and avoids router-specific cookies. |
| **HPE iLO 2 / 3 / 4** | ProLiant server controllers | Limits concurrent connections to 2 sockets to avoid `SessionExhaustedError`. |
| **APC UPS (NMC1 / NMC2)** | Older APC network management cards | Limits concurrent connections to 2 sockets for slower embedded cards. |
| **Cisco / HP switch or firewall** | Catalyst, SG300, ASA, ProCurve, ZyXEL | Uses legacy TLS directly and omits home-router cookies. |
| **Printer, storage device, or camera** | HP printers, QNAP/Synology/ReadyNAS, IP cameras | Standard 4-socket limit with clean headers. |

---

## Reopening or Quitting the App

Keep the opener tab open while you use the router page. Closing the last opener tab stops the bridge and disconnects any router tabs after a few seconds (4 seconds). You can also click **Quit app** and confirm. Double-click `legacy-router-opener.exe` to open it again.

When using `-no-browser` or `--open-router` (or `-open-router`), the bridge waits until you stop it with `Ctrl+C` in the terminal or visit the opener URL printed in the terminal output (for example, `http://localhost:8180/_bridge`) and click **Quit app**. Once you open an opener tab, closing the last one stops the bridge after 4 seconds.

If you ever need a separate instance on a different port, run it from a terminal with `-port`:
```powershell
.\legacy-router-opener.exe -port 8090
```

---

## TLS & Protocol Compatibility

The standalone Windows executable (`legacy-router-opener.exe`, built with Go 1.27 stdlib) handles almost all legacy embedded devices without external runtimes, with two cryptographic exceptions:

- **Supported:** TLS 1.0–1.3, 1024-bit RSA keys, SHA-1 signatures, expired/self-signed/negative-serial certificates, `3DES`, `RC4`, and `AES-CBC` cipher suites, server-initiated renegotiation (both RFC 5746 and unsafe legacy renegotiation), abrupt TCP close without TLS `close_notify`, compact ClientHello fallback, and automatic `:443` → `:80` / plain HTTP fallback.
- **Not supported by the Go stdlib `.exe`:** finite-field `DHE-RSA` cipher suites (used by some Dell iDRAC 6 configurations) and `SSLv3` (used by older APC NMC1 cards such as `AP9617`).
  - If your device requires `DHE-RSA` or `SSLv3`, try plain HTTP (`http://<device-ip>` on port `80`) if enabled on the device, or run the included OpenSSL-backed Node.js fallback at [`desktop/legacy/index.mjs`](../legacy/index.mjs).
