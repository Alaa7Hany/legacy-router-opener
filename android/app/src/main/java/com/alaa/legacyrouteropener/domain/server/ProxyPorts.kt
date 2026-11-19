package com.alaa.legacyrouteropener.domain.server

import com.alaa.legacyrouteropener.domain.model.AppLanguage
import com.alaa.legacyrouteropener.domain.model.Endpoint
import com.alaa.legacyrouteropener.domain.model.HardwarePreset
import com.alaa.legacyrouteropener.domain.model.TlsInfo
import java.io.Closeable

interface PreferencesStore {
    fun loadTarget(): String?
    fun saveTarget(target: String, presetId: String)
    fun loadPresetId(): String?
    fun loadLanguage(): AppLanguage?
    fun saveLanguage(language: AppLanguage)
}

interface GatewayProvider {
    fun detectGatewayIp(): String?
}

interface ProxyServiceController {
    fun start(target: String)
    fun stop()
}

interface SessionCookieCleaner {
    fun clear(url: String, cookies: List<Pair<String, String>>)
}

interface ProxySession : Closeable {
    val endpoint: Endpoint
    val preset: HardwarePreset
    val url: String
    fun start()
    fun retiredCookies(): List<Pair<String, String>>
}

interface ProxySessionFactory {
    fun create(
        endpoint: Endpoint,
        preset: HardwarePreset,
        language: () -> AppLanguage,
        message: (AppLanguage, String) -> String,
        telemetry: (TlsInfo) -> Unit,
        log: (String) -> Unit
    ): ProxySession?
}
