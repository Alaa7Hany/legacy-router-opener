# Troubleshooting & Device Profiles

Quick answers when a router or legacy device refuses to log in or load properly.

---

## "Duplicated Login" or Stuck Login Screen

Older Huawei/ZTE routers and HPE iLO controllers only allow **one active admin session** at a time, and some devices keep stale cookies stuck in your browser.

**How to fix:**
1. Go back to the **Router Page Opener** tab. If you closed it, double-click `legacy-router-opener.exe` to reopen it.
2. Click **Reset session** directly beneath the main button.
3. This clears the bridge's cookie jar, tells your browser to drop stale `localhost` cookies, and flushes cached pages.
4. Click **Open Router Page** again.

> **Tip:** If you use another local tool that also connects to your router's admin account at the same time, disconnect that tool first so the router doesn't kick you out with a duplicate-login error.

---

## Opening a Device That Isn't at `192.168.1.1`

The app auto-detects your computer's default gateway on startup. If you want to open another device (such as a secondary router on `192.168.0.1`, a switch, a network printer, or a server controller):

1. Click **Change IP or device type** on the opener card to expand the options.
2. Enter the address in **Router or device address**:
   - Plain IP (tries HTTPS 443, falls back to HTTP 80): `192.168.0.1`
   - Explicit plain HTTP: `http://192.168.1.254`
   - Custom port: `192.168.0.120:8443`
3. Click **Apply changes** or click the main **Open Router Page** button.
4. To jump back to your main Wi-Fi router anytime, click **Use detected (`192.168.1.1`)** above the address box.

---

## Device Compatibility Profiles

Expand **Change IP or device type** to choose a profile if a specific piece of hardware is picky:

| Profile | Best For | What It Adjusts |
| :--- | :--- | :--- |
| **Auto / Universal** | Most home routers and unknown devices | Starts with balanced defaults and automatically adapts to HTTP or legacy TLS if needed. |
| **Huawei / ZTE ISP Router** | Older ISP DSL/VDSL routers (`HG531`, `HG532n`, `H108N`, `H168N`) | Uses legacy TLS immediately and pre-seeds required menu/language cookies. |
| **Dell iDRAC 6 / 7** | Older PowerEdge server management | Rewrites internal hostname redirects and avoids router-specific cookies. |
| **HPE iLO 2 / 3 / 4** | ProLiant server controllers | Limits concurrent connections to 2 sockets to avoid `SessionExhaustedError`. |
| **APC UPS NMC1 / NMC2** | Older APC network management cards | Limits concurrent connections to 2 sockets for slower embedded cards. |
| **Cisco / HP Switch & Firewall** | Catalyst, SG300, ASA, ProCurve, ZyXEL | Uses legacy TLS directly and omits home-router cookies. |
| **Printer EWS / Legacy NAS / Cam** | HP printers, QNAP/Synology/ReadyNAS, IP cameras | Standard 4-socket limit with clean headers. |

---

## Reopening or Quitting the App

Closing the browser tab leaves the bridge running. Double-click `legacy-router-opener.exe` to reopen the existing instance, even if it started on a different port because `8080` was busy.

To stop the bridge and disconnect any router tabs opened through it, click **Quit app** in the opener and confirm.

If you ever need a separate instance on a different port, run it from a terminal with `-port`:
```powershell
.\legacy-router-opener.exe -port 8090
```
