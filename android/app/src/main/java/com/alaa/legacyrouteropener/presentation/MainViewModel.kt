package com.alaa.legacyrouteropener.presentation

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import com.alaa.legacyrouteropener.data.model.ProxyState
import com.alaa.legacyrouteropener.data.network.DirectRouterHttpClient
import com.alaa.legacyrouteropener.domain.server.ProxyServer
import com.alaa.legacyrouteropener.domain.util.GatewayDetector
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import java.io.IOException

class MainViewModel(application: Application) : AndroidViewModel(application) {

    private val _state = MutableStateFlow(ProxyState())
    val state: StateFlow<ProxyState> = _state.asStateFlow()

    private var proxyServer: ProxyServer? = null
    private val httpClient = DirectRouterHttpClient()

    init {
        detectNetwork()
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
        proxyServer?.routerIp = ip
    }

    fun startProxy() {
        val currentState = _state.value
        try {
            proxyServer?.stop()
            proxyServer = ProxyServer(
                proxyHost = "127.0.0.1",
                proxyPort = currentState.proxyPort,
                routerIp = currentState.routerIp,
                httpClient = httpClient,
                onLog = { addLog(it) }
            ).apply {
                start()
            }

            _state.update { it.copy(isRunning = true) }
            addLog("🚀 Proxy active on http://127.0.0.1:${currentState.proxyPort}")
            addLog("🎯 Target Router: https://${currentState.routerIp}:443")

        } catch (e: IOException) {
            addLog("❌ Failed to start proxy: ${e.message}")
            _state.update { it.copy(isRunning = false) }
        }
    }

    fun stopProxy() {
        proxyServer?.stop()
        proxyServer = null
        _state.update { it.copy(isRunning = false) }
        addLog("⏹️ Proxy stopped.")
    }

    fun clearLogs() {
        _state.update { it.copy(logs = emptyList()) }
    }

    private fun addLog(message: String) {
        _state.update { current ->
            val updatedLogs = (current.logs + message).takeLast(200)
            current.copy(logs = updatedLogs)
        }
    }

    override fun onCleared() {
        proxyServer?.stop()
        super.onCleared()
    }
}
