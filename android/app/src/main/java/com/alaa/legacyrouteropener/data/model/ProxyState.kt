package com.alaa.legacyrouteropener.data.model

data class ProxyState(
    val isRunning: Boolean = false,
    val routerIp: String = "192.168.1.1",
    val proxyPort: Int = 8080,
    val wifiSsid: String? = null,
    val logs: List<String> = emptyList()
)
