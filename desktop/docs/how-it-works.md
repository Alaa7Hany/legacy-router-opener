# How the Desktop Bridge Works

Modern web browsers (Chrome, Edge, Firefox, Safari) have tightened their security rules over the years. While that protects public websites, it completely blocks access to older routers, switches, printers, and server controllers that no longer receive firmware updates.

This app runs two small local HTTP servers on your own computer: the **opener & control interface** (usually at `http://localhost:8180/_bridge`) and the **router proxy** (usually at `http://localhost:8080/`). Keeping them on separate `localhost` origins isolates your control settings from any scripts served by an older router. It translates between your modern browser and your older device on the local network.

---

## Why Modern Browsers Block `192.168.1.1`

1. **Outdated Encryption (TLS 1.0 / 1.1 & Older Ciphers):**
   Modern browsers permanently reject older TLS versions and ciphers, showing errors like `ERR_SSL_VERSION_OR_CIPHER_MISMATCH` or `SSL_ERROR_UNSUPPORTED_VERSION`.
   - **What the bridge does:** Negotiates the older TLS protocols and ciphers directly with the device on your behalf while serving the page cleanly to your browser over `localhost`.

2. **Forced HTTPS Upgrades:**
   Browsers automatically try to upgrade `http://192.168.1.1` to `https://`. Devices that only listen on plain HTTP port 80 reset the connection (`ERR_CONNECTION_CLOSED`).
   - **What the bridge does:** Browsers treat `http://localhost` as a safe local exception. When a device refuses HTTPS on port 443, the bridge automatically falls back to plain HTTP on port 80.

3. **Dropped Login Cookies (`Secure` Flag):**
   Some routers mark login cookies as `Secure`, which browsers ignore over plain HTTP. That causes the login page to refresh in an endless loop even with the right password.
   - **What the bridge does:** Keeps a server-side session cookie jar and strips incompatible cookie flags (`Secure`, `Domain`, `SameSite=None`) before passing them to your browser.

4. **Hardcoded Redirects & Framesets:**
   Older admin dashboards often contain scripts like `top.location.href = 'https://192.168.1.1/'`, which kick your browser out of any proxy back to the blocked IP.
   - **What the bridge does:** Rewrites hardcoded device addresses inside redirect headers, HTML pages, and scripts so navigation stays inside `localhost`.

5. **Overloaded Router Processors:**
   Opening a single login page can trigger 15–20 simultaneous image and script requests. Older embedded CPUs drop connections (`socket hang up` or `SessionExhaustedError`) when hit with that many handshakes at once.
   - **What the bridge does:** Caps simultaneous connections to the device (2–4 at a time), caches static icons/scripts in memory, and retries transient resets automatically.

---

## Does This Slow Down My Internet or Modify My Browser?

**No.**
- It does not weaken or change any settings in Chrome, Edge, or Firefox.
- It only handles traffic to the router or device you open through the local bridge. Normal internet browsing never passes through this app.
- Nothing is sent to external servers; everything stays strictly between your computer and your local network device.
