# Android Troubleshooting

Fixes for issues specific to Android phones. For device compatibility profiles (Huawei/ZTE, Dell iDRAC, HPE iLO, APC, Cisco/HP, printers) and general "Duplicated Login" tips, see [Desktop Troubleshooting & Device Profiles](../../desktop/docs/troubleshooting-and-device-modes.md).

---

## External Browser Keeps Loading or Stops Responding

1. **Check Battery Restrictions:** On some phones (such as Samsung's *Sleeping / Deep sleeping apps* or Xiaomi's *Battery saver*), strict per-app battery limits can still freeze background apps. Open your phone's **App info → Battery** for *Router Page Opener* and set it to **Optimized** or **Unrestricted**.
2. **Don't Force-Stop the App:** Swiping away or force-stopping *Router Page Opener* shuts down the local bridge, which disconnects any open router tabs in Chrome or Edge.
3. **Keep the Session Fresh:** If you tapped **Fix stuck login** or **Stop local proxy**, old browser tabs from the previous session will intentionally stop working—open a fresh tab from the app.

---

## Router Page Won't Open Even Though Wi-Fi Is Connected

1. **Turn Off Any Active VPN:** VPN apps often route all traffic away from local addresses like `192.168.1.1`. Pause your VPN temporarily while configuring your router.
2. **Wi-Fi Without Internet Switching to Mobile Data:** When a router has no internet, Android may automatically send traffic over 4G/5G instead of Wi-Fi. Temporarily toggle **Mobile data off** (or accept Android's *"Stay connected without internet"* prompt) so your phone talks to the router over Wi-Fi.
3. **Wrong Address Auto-Detected:** Expand **Change device** and verify the IP matches the label on your router (common alternatives are `192.168.0.1`, `192.168.1.254`, or `10.0.0.1`). Tap **Use detected address** to refill your current Wi-Fi gateway, then tap **Apply changes**.

---

## Login Page Loads, But Signing In Fails or Loops

1. Tap **Fix stuck login** on the main screen to wipe saved cookies and start a clean session, then open the router page again.
2. Expand **Change device** and try picking the matching **Device type** (for example, **Huawei / ZTE router** for older ISP routers), then tap **Apply changes**.
3. Make sure no other phone or computer is currently signed into the router's admin page—many older routers only allow one admin login at a time.

---

## Can't See the "Local proxy is active" Notification

On Android 13 and newer, notifications are hidden unless allowed for the app. The local proxy still stays active without it, but if you want the quick **Stop local proxy** button in your notification shade, enable notifications in **App info → Notifications**.

---

## How to Stop the Local Proxy

You can stop the bridge at any time in two ways:
- Tap **Stop local proxy** at the bottom of the main screen inside the app.
- Expand the silent *Router Page Opener* notification and tap **Stop local proxy**.

Stopping the proxy immediately closes the local bridge, disconnects open router tabs, and clears saved login cookies.
