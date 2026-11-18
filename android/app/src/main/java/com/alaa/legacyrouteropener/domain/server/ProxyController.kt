package com.alaa.legacyrouteropener.domain.server

import android.content.Context
import com.alaa.legacyrouteropener.data.i18n.BridgeMessages
import com.alaa.legacyrouteropener.data.model.Endpoint
import com.alaa.legacyrouteropener.data.model.HardwarePreset
import com.alaa.legacyrouteropener.data.model.ProxyState
import com.alaa.legacyrouteropener.data.network.DirectRouterHttpClient
import com.alaa.legacyrouteropener.data.network.MemoryBudget
import com.alaa.legacyrouteropener.data.network.SocketRegistry
import com.alaa.legacyrouteropener.domain.util.GatewayDetector
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import java.io.IOException
import java.net.BindException
import java.security.SecureRandom
import java.util.Locale

class ProxyController internal constructor(
    context: Context,
    private val messages: BridgeMessages,
    private val onRetired: (String, List<Pair<String, String>>) -> Unit = { _, _ -> }
) {
    private val preferences = context.getSharedPreferences("opener", Context.MODE_PRIVATE)
    private val gateway = GatewayDetector.detectGatewayIp(context)
    private val memory = MemoryBudget()
    private val rng = SecureRandom()
    private val mutable = MutableStateFlow(ProxyState(
        routerIp = preferences.getString("target", null) ?: gateway ?: "192.168.1.1",
        detectedGateway = gateway,
        presetId = HardwarePreset.find(preferences.getString("preset", null)).id,
        language = preferences.getString("language", null)
            ?: if (context.resources.configuration.locales[0].language == "ar") "ar" else "en"
    ))
    val state = mutable.asStateFlow()

    private data class TargetSession(
        val endpoint: Endpoint,
        val preset: HardwarePreset,
        val url: String,
        val listener: LocalHttpServer,
        val sockets: SocketRegistry,
        val proxy: ProxyServer
    ) {
        fun close() {
            sockets.close()
            listener.close()
        }
    }

    private var active: TargetSession? = null

    fun changeAddress(address: String) = mutable.update {
        it.copy(routerIp = address.take(1024), validationError = null, banner = null)
    }

    fun choosePreset(preset: HardwarePreset) = mutable.update {
        it.copy(presetId = preset.id, validationError = null, banner = null)
    }

    fun useDetected() = mutable.update {
        val address = it.detectedGateway
        if (address == null) it.copy(banner = "detection_failed")
        else it.copy(routerIp = address, validationError = null, banner = "detected_saved")
    }

    fun chooseLanguage(language: String) {
        if (language !in setOf("en", "ar")) return
        mutable.update { it.copy(language = language) }
        preferences.edit().putString("language", language).apply()
    }

    fun clearLogs() = mutable.update { it.copy(logs = emptyList()) }

    @Synchronized
    fun open(): String? = applyTarget(start = true, announce = false)

    @Synchronized
    fun applyChanges() {
        applyTarget(start = active != null, announce = true)
    }

    @Synchronized
    fun resetLogin() {
        val current = active
        if (current == null) {
            mutable.update { it.copy(banner = "login_fixed", validationError = null) }
            return
        }
        if (replace(current.endpoint, current.preset) == null) {
            mutable.update { it.copy(banner = "login_fix_failed") }
        } else {
            mutable.update { it.copy(banner = "login_fixed", validationError = null) }
        }
    }

    private fun applyTarget(start: Boolean, announce: Boolean): String? {
        val draft = mutable.value
        val endpoint = try {
            Endpoint.parse(draft.routerIp)
        } catch (failure: IllegalArgumentException) {
            mutable.update { it.copy(validationError = failure.message ?: "address_invalid") }
            return null
        }
        val preset = HardwarePreset.find(draft.presetId)
        val current = active
        val address = if (!start) null else if (current?.endpoint == endpoint && current.preset == preset) current.url
            else replace(endpoint, preset)
        if (start && address == null) {
            mutable.update { it.copy(banner = "changes_failed") }
            return null
        }
        preferences.edit().putString("target", draft.routerIp.trim()).putString("preset", preset.id).apply()
        mutable.update { it.copy(validationError = null, banner = if (announce) "changes_saved" else null) }
        return address
    }

    private fun replace(endpoint: Endpoint, preset: HardwarePreset): String? {
        val hostname = ByteArray(16).also(rng::nextBytes).joinToString("") { byte ->
            "%02x".format(Locale.ROOT, byte.toInt() and 255)
        } + ".localhost"
        lateinit var proxy: ProxyServer
        val handler = { request: com.alaa.legacyrouteropener.data.model.RouterRequest -> proxy.serve(request) }
        val listener = try {
            try {
                LocalHttpServer(hostname, 8080, memory, handler)
            } catch (_: BindException) {
                LocalHttpServer(hostname, 0, memory, handler)
            }
        } catch (_: IOException) {
            return null
        }
        val authority = "$hostname:${listener.port}"
        val url = "http://$authority/"
        val sockets = SocketRegistry()
        val client = DirectRouterHttpClient(endpoint, preset, memory, sockets, listener.port, "http://$authority")
        proxy = ProxyServer(endpoint, authority, preset, memory, client,
            language = { mutable.value.language },
            message = { lang, key -> messages[lang, key] },
            telemetry = { value -> mutable.update { if (it.activeUrl == url) it.copy(tlsInfo = value) else it } },
            log = { line -> mutable.update { if (it.activeUrl == url) it.copy(logs = (it.logs + line).takeLast(150)) else it } }
        )
        val old = active
        active = TargetSession(endpoint, preset, url, listener, sockets, proxy)
        mutable.update { it.copy(activeUrl = url, activeTarget = endpoint.authority, proxyPort = listener.port,
            tlsInfo = null, logs = listOf("[PROXY] ${endpoint.authority}")) }
        old?.let(::retire)
        listener.start()
        return url
    }

    @Synchronized
    fun stop() {
        val current = active
        active = null
        mutable.update { it.copy(activeUrl = null, activeTarget = null, tlsInfo = null, banner = null) }
        current?.let(::retire)
    }

    private fun retire(session: TargetSession) {
        session.close()
        onRetired(session.url, session.proxy.retiredCookies())
    }
}
