# 🌐 Legacy Router Page Opener

> **A local TLS-bypass reverse proxy & mobile app for archaic TE Data, Huawei (HG531 V1 / HG532n), and ZTE (ZXHN) router administration dashboards.**

Available as:
* 💻 **Desktop / Windows Proxy** (Node.js 1-click batch launcher)
* 📱 **Android App** (Standalone Jetpack Compose + Embedded Proxy & In-App WebView)

---

## 📖 The Problem

Legacy ADSL/VDSL routers distributed by ISPs in Egypt (like TE Data / WE) manufactured between 2010 and 2017 cannot be accessed from modern web browsers (Chrome, Edge, Firefox, Safari) on laptops or smartphones.

### Root Causes
1. **Forced Automatic HTTPS:** Modern browsers automatically upgrade `http://192.168.1.1` to `https://192.168.1.1`. When the browser attempts an SSL/TLS handshake on port 80, the router immediately resets the TCP socket (`ERR_CONNECTION_CLOSED`).
2. **Archaic SSL Renegotiation (RFC 5746 violation):** The router's HTTPS server uses obsolete SSL/TLS ciphers requiring *Unsafe Legacy Renegotiation*. OpenSSL 3.0+ and all modern browser engines strictly block this:
   ```text
   write EPROTO 844D0000:error:0A000152:SSL routines:final_renegotiate:unsafe legacy renegotiation disabled
   ```
3. **Hardcoded JavaScript Redirects:** The router's HTML contains client-side scripts (e.g. `top.location.href = 'http://192.168.1.1/...'`) that break out of proxies back to raw IP.
4. **Secure Cookie Dropping on HTTP:** The router sets session cookies with the `Secure` flag. Browsers reject `Secure` cookies over plain HTTP on `localhost`, causing immediate authentication drops.
5. **Embedded CPU Overload:** Requesting 15+ static assets (icons, CSS, GIF images) simultaneously triggers 15 parallel TLS handshakes, exhausting the router's small MIPS processor and dropping sockets (`socket hang up`).

---

## 🛠️ The Technical Journey & Breakthroughs

| Step | Obstacle Encountered | Technical Solution Implemented |
| :--- | :--- | :--- |
| **1. Direct Access** | Edge forces HTTPS on port 80 ➔ `ERR_CONNECTION_CLOSED` | Created local proxy on `http://localhost:8080` (browsers never enforce HTTPS on localhost). |
| **2. Proxy Redirects** | Router returned `307 Temporary Redirect` to `https://192.168.1.1` | Implemented recursive redirect follower on the backend. |
| **3. Hardcoded Scripts** | Browser executed hardcoded IP JS redirects and jumped back to raw IP | Added on-the-fly regex string substitution for HTML and JS response bodies. |
| **4. OpenSSL 3.0 Block** | `error:0A000152: unsafe legacy renegotiation disabled` | Injected OpenSSL legacy flags: `SSL_OP_LEGACY_SERVER_CONNECT (0x4)` and `SSL_OP_ALLOW_UNSAFE_LEGACY_RENEGOTIATION (0x40000)` with `@SECLEVEL=0`. |
| **5. Login POST Hangup** | Port 80 drops socket on `POST /index/login.cgi` | Switched backend requests to direct HTTPS on Port 443 with connection pooling (`maxSockets: 4`, `keepAlive: true`). |
| **6. Post-Login Loop** | Huawei frameset dropped session because `Secure` cookies were rejected | Built a **Server-Side In-Memory Cookie Jar** inside the proxy and stripped `Secure` / `SameSite` flags before forwarding to the browser. |
| **7. Repeating Wizard** | Factory reset cleared ISP PPPoE credentials (`0000@tedata.net.eg`) | Configured the router with real TE Data/WE PPPoE credentials (VPI `0` / VCI `35`), permanently restoring internet connectivity. |

---

## 📱 Android App (`/android`)

The Android app provides a 100% mobile, standalone solution. It runs an embedded proxy inside Android using OkHttp with custom SSL socket factories to negotiate with the router, rendering the admin dashboard seamlessly in Jetpack Compose or your mobile browser.

### Features
* 🔍 **Auto-detects Wi-Fi Gateway IP** (DHCP gateway / `192.168.1.1`).
* 🚀 **In-App WebView** with JavaScript alert/dialog support and frameset navigation.
* 🌐 **"Open in Chrome"** button to view in external browser via `http://127.0.0.1:8080`.
* 📜 **Live TLS Activity Log Terminal**.

### How to Run / Build in Android Studio
1. Open **Android Studio**.
2. Select **File $\rightarrow$ Open** and choose the `android` folder in this repository.
3. Allow Gradle to sync and click **Run** (or `Build -> Build APK`).

---

## 💻 Desktop Quick Start (Windows / Mac / Linux)

### Prerequisites
- **Node.js** (v18 or higher) installed on your machine.
- Laptop connected to the router via **Wi-Fi** or **Ethernet cable**.

### Steps
1. **Option A (Double-Click on Windows):** Double-click `start.bat`.
2. **Option B (Command Line):**
   ```bash
   npm start
   ```
3. Open your browser and navigate to:
   👉 **`http://localhost:8080`**
4. Log in with your router credentials (e.g. `admin` / `admin` or password printed on the router label).

---

## 📋 ISP & Router Quick Reference

- **Default Gateway:** `192.168.1.1`
- **Router Web Login:** `admin` / (Label password or printed MAC/SN)
- **ADSL Connection Type:** `PPPoE`
- **VPI / VCI (TE Data / WE Egypt):** `0` / `35`
- **PPP Account Username:** Landline phone number with area code (e.g. `02XXXXXXXX@tedata.net.eg`)
- **PPP Password:** Sent via SMS from WE (or retrieved from the *My WE* mobile app).
