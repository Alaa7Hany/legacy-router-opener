# Privacy Policy

**Last updated:** November 20, 2026  
**Applies to:** Legacy Device & Router Opener (Windows, Linux, and Android)

Legacy Device & Router Opener runs **100% locally on your device** (`localhost` / `127.0.0.1`). It collects, stores, logs, or transmits **zero** personal data, analytics, telemetry, crash reports, advertising identifiers, or router credentials to any external server.

## Network Traffic

- All proxied network traffic stays strictly between your device and the local IP address or hostname entered by you.
- The app makes zero background external network requests. Optional links (`Website` and `GitHub`) open in your system browser only when tapped.

## On-Device Storage

- **Android:** Your selected device address, hardware profile, and UI language are saved locally on your device (`SharedPreferences`, with cloud backup disabled via `android:allowBackup="false"`). Active router session cookies are kept in memory only and cleared when you reset login or stop the proxy.
- **Windows & Linux:** Session cookies and activity logs live only in memory while the app is running and are discarded on exit. Your browser stores only your UI language preference in local storage on `localhost`.

## Android Permissions

- `INTERNET`, `ACCESS_NETWORK_STATE`, `ACCESS_WIFI_STATE`: Used solely to detect your local Wi-Fi gateway IP and bridge connections to your local device.
- `FOREGROUND_SERVICE`, `POST_NOTIFICATIONS`: Keeps the local proxy active when opening pages in an external browser and provides a quick stop action.

## Open Source & Contact

- Source code (GNU GPLv3): https://github.com/Alaa7Hany/legacy-router-opener
- Privacy Policy URL: https://3laa.me/legacy-router-opener/privacy.html
- Contact: elgebaly3laa@gmail.com
