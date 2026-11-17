package com.alaa.legacyrouteropener.presentation

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import com.alaa.legacyrouteropener.data.model.ProxyState
import com.alaa.legacyrouteropener.data.network.DirectRouterHttpClient
import com.alaa.legacyrouteropener.data.repository.SessionCookieJar
import com.alaa.legacyrouteropener.domain.server.ProxyServer
import com.alaa.legacyrouteropener.domain.util.GatewayDetector
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import java.io.IOException

class MainViewModel(application: Application) : AndroidViewModel(application) {

    companion object {
        private const val DEFAULT_PROXY_PORT = 8080
        private const val MAX_PORT_ATTEMPTS = 10
    }

    /**
     * Process-scoped runtime holder so the embedded NanoHTTPD proxy and session cookies
     * survive Activity recreation or backgrounding while WebViewActivity or Chrome is active.
     */
    private object ProxyRuntime {
        val httpClient = DirectRouterHttpClient()
        val cookieJar = SessionCookieJar()
        val logs = mutableListOf<String>()

        @Volatile
        var proxyServer: ProxyServer? = null

        @Volatile
        var logListener: ((List<String>) -> Unit)? = null

        @Synchronized
        fun appendLog(message: String) {
            logs.add(message)
            while (logs.size > 200) {
                logs.removeAt(0)
            }
            val snapshot = logs.toList()
            logListener?.invoke(snapshot)
        }

        @Synchronized
        fun clearLogs() {
            logs.clear()
            logListener?.invoke(emptyList())
        }

        @Synchronized
        fun getLogsSnapshot(): List<String> = logs.toList()
    }

    private val _state = MutableStateFlow(ProxyState())
    val state: StateFlow<ProxyState> = _state.asStateFlow()

    init {
        ProxyRuntime.logListener = { snapshot ->
            _state.update { it.copy(logs = snapshot) }
        }

        val activeServer = ProxyRuntime.proxyServer
        if (activeServer != null && activeServer.isAlive) {
            val context = getApplication<Application>()
            val ssid = GatewayDetector.getConnectedWifiSsid(context)
            _state.update {
                it.copy(
                    isRunning = true,
                    routerIp = activeServer.routerIp,
                    proxyPort = activeServer.proxyPort,
                    wifiSsid = ssid,
                    logs = ProxyRuntime.getLogsSnapshot()
                )
            }
        } else {
            _state.update { it.copy(logs = ProxyRuntime.getLogsSnapshot()) }
            detectNetwork()
        }
    }

    fun detectNetwork() {
        val context = getApplication<Application>()
        val gateway = GatewayDetector.detectGatewayIp(context)
        val ssid = GatewayDetector.getConnectedWifiSsid(context)

        _state.update {
            it.copy(
                routerIp = gateway,
                wifiSsid = ssid
            )
        }
        addLog("🔍 Auto-detected Wi-Fi Gateway: $gateway")
    }

    fun updateRouterIp(ip: String) {
        _state.update { it.copy(routerIp = ip) }
        val sanitized = sanitizeRouterEndpoint(ip)
        ProxyRuntime.proxyServer?.routerIp = sanitized
    }

    fun startProxy(): Int? {
        val currentState = _state.value
        val targetEndpoint = sanitizeRouterEndpoint(currentState.routerIp)
        ProxyRuntime.proxyServer?.stop()
        ProxyRuntime.proxyServer = null

        var lastError: IOException? = null
        for (offset in 0 until MAX_PORT_ATTEMPTS) {
            val candidatePort = DEFAULT_PROXY_PORT + offset
            try {
                val server = ProxyServer(
                    proxyHost = "127.0.0.1",
                    proxyPort = candidatePort,
                    routerIp = targetEndpoint,
                    httpClient = ProxyRuntime.httpClient,
                    cookieJar = ProxyRuntime.cookieJar,
                    onLog = { ProxyRuntime.appendLog(it) }
                ).apply {
                    start()
                }
                ProxyRuntime.proxyServer = server

                _state.update {
                    it.copy(
                        isRunning = true,
                        routerIp = targetEndpoint,
                        proxyPort = candidatePort
                    )
                }
                if (offset > 0) {
                    addLog("⚠️ Port $DEFAULT_PROXY_PORT was in use; fell back to port $candidatePort")
                }
                addLog("🚀 Proxy active on http://127.0.0.1:$candidatePort")
                addLog("🎯 Target Router: $targetEndpoint")
                return candidatePort
            } catch (e: IOException) {
                lastError = e
                if (offset == 0) {
                    addLog("⚠️ Port $candidatePort unavailable (${e.message}), trying fallback ports...")
                }
            }
        }

        addLog("❌ Failed to start proxy: ${lastError?.message ?: "No ports available"}")
        _state.update { it.copy(isRunning = false) }
        return null
    }

    fun stopProxy() {
        ProxyRuntime.proxyServer?.stop()
        ProxyRuntime.proxyServer = null
        ProxyRuntime.cookieJar.clear()
        _state.update { it.copy(isRunning = false, proxyPort = DEFAULT_PROXY_PORT) }
        addLog("⏹️ Proxy stopped.")
    }

    fun clearLogs() {
        ProxyRuntime.clearLogs()
    }

    private fun sanitizeRouterEndpoint(raw: String): String {
        val trimmed = raw.trim()
        if (trimmed.isEmpty()) return "192.168.1.1"
        val isHttp = trimmed.startsWith("http://", ignoreCase = true)
        val body = trimmed
            .replace(Regex("^https?://", RegexOption.IGNORE_CASE), "")
            .substringBefore('/')
            .trim()
            .ifEmpty { "192.168.1.1" }
        return if (isHttp) "http://$body" else body
    }

    private fun addLog(message: String) {
        ProxyRuntime.appendLog(message)
    }

    override fun onCleared() {
        ProxyRuntime.logListener = null
        super.onCleared()
    }
}
