package com.alaa.legacyrouteropener.data.proxy

import com.alaa.legacyrouteropener.domain.server.ProxySession
import com.alaa.legacyrouteropener.domain.server.ProxySessionFactory
import com.alaa.legacyrouteropener.domain.model.AppLanguage
import com.alaa.legacyrouteropener.domain.model.Endpoint
import com.alaa.legacyrouteropener.domain.model.HardwarePreset
import com.alaa.legacyrouteropener.domain.model.TlsInfo
import com.alaa.legacyrouteropener.data.network.DirectRouterHttpClient
import com.alaa.legacyrouteropener.data.network.MemoryBudget
import com.alaa.legacyrouteropener.data.network.SocketRegistry
import java.io.IOException
import java.net.BindException
import java.security.SecureRandom
import java.util.Locale

internal class DefaultProxySessionFactory(
    private val memory: MemoryBudget = MemoryBudget(),
    private val rng: SecureRandom = SecureRandom()
) : ProxySessionFactory {
    override fun create(
        endpoint: Endpoint,
        preset: HardwarePreset,
        language: () -> AppLanguage,
        message: (AppLanguage, String) -> String,
        telemetry: (TlsInfo) -> Unit,
        log: (String) -> Unit
    ): ProxySession? {
        val hostname = ByteArray(16).also(rng::nextBytes).joinToString("") { byte ->
            "%02x".format(Locale.ROOT, byte.toInt() and 255)
        } + ".localhost"
        val listener = try {
            try {
                LocalHttpServer(hostname, 8080, memory)
            } catch (_: BindException) {
                LocalHttpServer(hostname, 0, memory)
            }
        } catch (_: IOException) {
            return null
        }
        val authority = "$hostname:${listener.port}"
        val url = "http://$authority/"
        val sockets = SocketRegistry()
        val client = DirectRouterHttpClient(endpoint, preset, memory, sockets, listener.port, "http://$authority")
        val proxy = ProxyServer(endpoint, authority, preset, memory, client, language, message, telemetry, log)
        return LiveProxySession(endpoint, preset, url, listener, sockets, proxy)
    }

    private class LiveProxySession(
        override val endpoint: Endpoint,
        override val preset: HardwarePreset,
        override val url: String,
        private val listener: LocalHttpServer,
        private val sockets: SocketRegistry,
        private val proxy: ProxyServer
    ) : ProxySession {
        override fun start() {
            listener.start(proxy::serve)
        }

        override fun retiredCookies(): List<Pair<String, String>> = proxy.retiredCookies()

        override fun close() {
            sockets.close()
            listener.close()
            proxy.close()
        }
    }
}
