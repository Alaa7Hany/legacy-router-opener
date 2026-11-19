# Android Design & Platform Decisions

Choices specific to how the app is built for Android phones. For shared interface decisions (such as the single-action opener card, light/dark theme, and English/Arabic support), see [Desktop Design & Platform Decisions](../../desktop/docs/design-and-platform-decisions.md).

---

## 1. Offering Both an In-App Viewer and External Browsers

- **In-app viewer first:** Tapping **Open router page** opens the device immediately with no app switching, keeping the whole flow quick and self-contained.
- **External browser when you want it:** Many people keep router logins in a password manager or prefer their everyday browser's autofill, zoom, and download handling. **Open in external browser** hands off the same active session to whatever browser you use.

---

## 2. Keeping a Quiet Notification While the Proxy Is Active

- **Problem:** Modern Android (and phone makers like Samsung, Xiaomi, and OnePlus) quickly pauses apps and blocks their network connections the moment you switch away—especially when Battery Saver is turned on. Without telling Android that an active tool is running, switching to Chrome or Edge freezes the local bridge and leaves the browser stuck loading forever.
- **Decision:** Starting the local bridge runs a silent foreground notification so Android keeps the bridge awake and connected while you use an external browser. As soon as you tap **Stop local proxy** (in the app or directly in the notification), the bridge shuts down and the notification disappears.

---

## 3. A Fresh Local Address for Every Session

- **Problem:** If you switch from one router to another—or tap **Fix stuck login**—an old tab left open in your external browser could still hold stale cookies or accidentally send a form to the new device.
- **Decision:** Each proxy session gets its own temporary random `.localhost` address on your phone. When you change devices, reset a stuck login, or stop the proxy, the previous address stops working immediately so old tabs are cleanly cut off from the new session.

---

## 4. Bundled Legacy Encryption Instead of Android's Built-In Stack

- **Problem:** Android 10 and newer block `TLS 1.0`, `TLS 1.1`, `SSLv3`, weak Diffie-Hellman keys, and legacy ciphers at the operating-system level.
- **Decision:** Rather than asking users to install old browsers or change system settings, the app ships a dedicated legacy encryption library used exclusively to reach the local device you choose. Everything else on your phone continues using Android's modern security defaults.

---

## 5. Minimal Permissions & Offline-First

- **No internet required:** Just like the desktop version, the app works completely offline when your router has lost internet access.
- **Only what's needed:** The app asks only for local network access (to detect your Wi-Fi gateway and reach your router) and foreground-service support (so external browsers don't stall in the background). Notification permission on Android 13+ is optional—it lets you see the quick **Stop local proxy** shortcut, and the bridge still works even if you leave notifications off. No location permission, no camera/storage access, and no analytics.
