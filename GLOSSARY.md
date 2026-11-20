# Legacy Device & Router Opener

The language used across this project's docs and code: the local bridge that lets a modern browser reach legacy network devices, the interface that starts it, and the compatibility choices for those devices.

## Language

**Bridge**:
The local service that stands between a modern browser and a legacy device, translating the device's older TLS and HTTP behaviour into something the browser accepts.
_Avoid_: proxy, gateway, TLS shim

**Opener**:
The local start page used to launch the device page, change which device is targeted, or stop the bridge.
_Avoid_: dashboard, control panel, control page

**Device profile**:
A named compatibility choice that adjusts how the bridge talks to a particular family of hardware, such as an older router, a server console, or a printer.
_Avoid_: preset, mode, hardware profile
