package com.alaa.legacyrouteropener.data.model

data class ProxyState(
    val routerIp: String = "192.168.1.1",
    val detectedGateway: String? = null,
    val presetId: String = "auto",
    val language: String = "en",
    val activeUrl: String? = null,
    val activeTarget: String? = null,
    val proxyPort: Int = 8080,
    val tlsInfo: TlsInfo? = null,
    val validationError: String? = null,
    val banner: String? = null,
    val logs: List<String> = emptyList()
) {
    val isRunning get() = activeUrl != null
}
