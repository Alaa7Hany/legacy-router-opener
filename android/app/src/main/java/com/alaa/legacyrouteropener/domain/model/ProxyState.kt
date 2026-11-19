package com.alaa.legacyrouteropener.domain.model

data class ProxyState(
    val routerIp: String = "192.168.1.1",
    val detectedGateway: String? = null,
    val presetId: String = "auto",
    val language: AppLanguage = AppLanguage.EN,
    val activeUrl: String? = null,
    val activeTarget: String? = null,
    val tlsInfo: TlsInfo? = null,
    val validationError: ValidationError? = null,
    val banner: BannerMessage? = null
) {
    val isRunning get() = activeUrl != null
}
