package com.alaa.legacyrouteropener.data.network

import com.alaa.legacyrouteropener.data.model.Endpoint
import com.alaa.legacyrouteropener.data.model.HardwarePreset
import com.alaa.legacyrouteropener.data.model.RouterRequest
import com.alaa.legacyrouteropener.data.model.RouterResponse
import com.alaa.legacyrouteropener.data.model.TlsInfo
import org.bouncycastle.tls.TlsClientProtocol
import java.io.BufferedInputStream
import java.io.FilterInputStream
import java.io.IOException
import java.io.InputStream
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.Socket
import java.net.SocketException
import java.net.SocketTimeoutException
import java.util.concurrent.Semaphore
import java.util.concurrent.TimeUnit

internal class DirectRouterHttpClient(
    private val target: Endpoint,
    private val preset: HardwarePreset,
    private val budget: MemoryBudget,
    private val sockets: SocketRegistry,
    private val blockedPort: Int,
    private val origin: String
) : RouterHttpClient {
    private data class Attempt(val endpoint: Endpoint, val profile: TlsProfile?)
    private class BeforeWrite(val failure: IOException, val connected: Boolean, val plaintext: Boolean) : IOException()
    private val slots = Semaphore(preset.sockets, true)
    @Volatile private var preferred: Attempt? = null

    override fun send(request: RouterRequest, cookies: String): UpstreamResult {
        if (sockets.closed || !slots.tryAcquire(15, TimeUnit.SECONDS)) throw IOException("Upstream busy")
        try {
            val cached = preferred
            val attempts = ArrayDeque<Attempt>()
            if (cached != null) attempts.add(cached)
            val profiles = if (!target.tls) listOf(null) else if (preset.preferLegacyTls) {
                listOf(TlsProfile.LEGACY_STRIPPED, TlsProfile.MODERN, TlsProfile.SSLV3_ONLY)
            } else {
                listOf(TlsProfile.MODERN, TlsProfile.LEGACY_STRIPPED, TlsProfile.SSLV3_ONLY)
            }
            profiles.map { Attempt(target, it) }.filterNot { it == cached }.forEach(attempts::add)
            var last: IOException? = null
            val dns = InetAddress.getAllByName(target.host).filterNot {
                it.isAnyLocalAddress || it.isMulticastAddress || (it.isLoopbackAddress && target.port == blockedPort)
            }
            if (dns.isEmpty()) throw IOException("Target resolves to a blocked address")
            while (attempts.isNotEmpty() && !sockets.closed) {
                val attempt = attempts.removeFirst()
                try {
                    return connect(attempt, dns, request, cookies).also { preferred = attempt }
                } catch (failure: BeforeWrite) {
                    last = failure.failure
                    val endpoint = attempt.endpoint
                    val implicitHttp = !target.explicitScheme && !target.explicitPort &&
                        (last is java.net.ConnectException || last is SocketException || last is java.io.EOFException)
                    val customHttp = !target.explicitScheme && target.explicitPort && failure.plaintext
                    if (endpoint.tls && (implicitHttp || customHttp)) {
                        val fallback = Attempt(endpoint.copy(tls = false, port = if (implicitHttp) 80 else endpoint.port), null)
                        if (cached != fallback && fallback !in attempts) attempts.addLast(fallback)
                    }
                    if (attempt == cached) preferred = null
                    if (!failure.connected && last is SocketTimeoutException) break
                }
            }
            throw last ?: IOException("Session closed")
        } finally {
            slots.release()
        }
    }

    private fun connect(attempt: Attempt, addresses: List<InetAddress>, request: RouterRequest, cookies: String): UpstreamResult {
        val endpoint = attempt.endpoint
        val socket = sockets.track(Socket())
        var connected = false
        var startedWrite = false
        var inspection: HandshakeInput? = null
        try {
            // Use the vetted numerical address for the dial. The HTTP Host and TLS SNI use the configured hostname.
            if (endpoint.port == blockedPort && addresses.first().isLoopbackAddress) throw IOException("Self-proxy blocked")
            socket.connect(InetSocketAddress(addresses.first(), endpoint.port), 10_000)
            connected = true
            val rawInput = BufferedInputStream(socket.getInputStream())
            val rawOutput = socket.getOutputStream()
            val tls = attempt.profile?.let { profile ->
                val probe = HandshakeInput(rawInput)
                inspection = probe
                val protocol = TlsClientProtocol(probe, rawOutput)
                val client = LegacyRouterTlsClient(endpoint, profile)
                SocketDeadline(socket, 10_000).use { deadline ->
                    deadline.check()
                    protocol.connect(client)
                }
                protocol to client.info()
            }
            val output = tls?.first?.outputStream ?: rawOutput
            val input = BufferedInputStream(tls?.first?.inputStream ?: rawInput)
            val headers = HttpWireCodec.forwardedHeaders(request.headers).toMutableMap()
            headers.remove("host")
            headers.remove("cookie")
            headers["host"] = listOf(endpoint.authority)
            headers["connection"] = listOf("close")
            headers["accept-encoding"] = listOf("identity")
            if (headers.first("origin") == origin) headers["origin"] = listOf(endpoint.url)
            headers.first("referer")?.takeIf { it.startsWith("$origin/") }?.let {
                headers["referer"] = listOf(endpoint.url + it.removePrefix(origin))
            }
            if (cookies.isNotEmpty()) headers["cookie"] = listOf(cookies)
            if (request.body.size > 0 || request.method in setOf("POST", "PUT", "PATCH") ||
                request.headers.containsKey("content-length") || request.headers.containsKey("transfer-encoding")) {
                headers["content-length"] = listOf(request.body.size.toString())
            }
            SocketDeadline(socket, 15_000).use { deadline ->
                deadline.check()
                startedWrite = true
                HttpWireCodec.writeHead(output, "${request.method} ${request.path} HTTP/1.1", headers)
                request.body.writeTo(output)
                output.flush()
            }
            request.body.close()
            val head = SocketDeadline(socket, 15_000).use {
                it.check()
                HttpWireCodec.readResponse(input)
            }
            val body = if (HttpWireCodec.hasBody(request.method, head.status)) {
                SocketDeadline(socket, 30_000).use {
                    it.check()
                    HttpWireCodec.body(input, head.headers, budget, closeDelimited = true)
                }
            } else WireBody(budget)
            val telemetry = tls?.second ?: TlsInfo(endpoint.authority, "HTTP")
            return UpstreamResult(RouterResponse(head.status, head.headers, body), endpoint, telemetry)
        } catch (failure: IOException) {
            if (startedWrite || sockets.closed) throw failure
            throw BeforeWrite(failure, connected, inspection?.isPlaintext() == true)
        } finally {
            sockets.forget(socket)
            runCatching { socket.close() }
        }
    }
}

private class HandshakeInput(stream: InputStream) : FilterInputStream(stream) {
    private val prefix = ByteArray(16)
    private var size = 0

    override fun read(): Int = super.read().also { if (it >= 0 && size < prefix.size) prefix[size++] = it.toByte() }

    override fun read(bytes: ByteArray, offset: Int, length: Int): Int =
        super.read(bytes, offset, length).also { count ->
            if (count > 0 && size < prefix.size) {
                val copied = minOf(count, prefix.size - size)
                bytes.copyInto(prefix, size, offset, offset + copied)
                size += copied
            }
        }

    fun isPlaintext(): Boolean {
        val text = String(prefix, 0, size, Charsets.US_ASCII).lowercase()
        return text.startsWith("http/") || text.startsWith("<html") || text.startsWith("<!doc")
    }
}
