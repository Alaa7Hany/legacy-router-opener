# How the Android App Works

The Android app solves the same browser-blocking problems explained in [How the Desktop Bridge Works](../../desktop/docs/how-it-works.md), using a small local bridge that runs directly on your phone.

Nothing passes through the internet or any external server—traffic stays strictly between your phone and the router or device on your local Wi-Fi.

---

## Opening a Router Page: Two Options

1. **Open router page (In-App Viewer):**
   Opens your device directly inside the app with pinch-to-zoom support for older desktop-sized router screens. The viewer stays locked to your active router session so old pages cannot wander off to external websites.
2. **Open in external browser (Chrome, Edge, Firefox, Samsung Internet):**
   Opens the same local bridge in your phone's default browser. This is useful if you rely on your browser's password manager, want multiple tabs, or prefer downloading router backups/logs directly through your browser.

---

## What Happens Under the Hood on Android

- **Self-Contained Legacy Encryption:** Modern Android versions permanently removed older security protocols (`TLS 1.0`, `TLS 1.1`, `SSLv3`, and older key/cipher methods). The app includes its own built-in encryption layer just for talking to your local hardware, without weakening your phone's security for any other app or website. It even covers older `SSLv3` and `DHE` devices that the standalone Windows `.exe` cannot open directly.
- **Automatic Wi-Fi Gateway Detection:** When you open the app on Wi-Fi, it checks your current network's gateway address (usually `192.168.1.1`) so you do not have to type it manually.
- **Handling Quirky Router Firmware:** Older routers often have wrong internal clocks (for example, stuck in the year 2000), drop connections abruptly, use nested framesets, or send cookies that only work on one path. The app smooths these over so you can sign in and stay logged in without endless loading loops.
- **Quiet Active Notification:** While the local bridge is running, Android displays a silent notification showing which device is active and offering a one-tap **Stop local proxy** button.

---

## Does This Need Root, a VPN, or Browser Changes?

**No.**
- It does not install a VPN profile, root anything, or change settings in Chrome, Edge, or Android.
- Normal apps and internet browsing never pass through this app.
- Stopping the proxy shuts down the local bridge immediately and clears saved session cookies.
