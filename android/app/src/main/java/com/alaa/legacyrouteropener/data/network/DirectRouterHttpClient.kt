package com.alaa.legacyrouteropener.data.network

import com.alaa.legacyrouteropener.domain.model.Endpoint
import com.alaa.legacyrouteropener.domain.model.HardwarePreset
import com.alaa.legacyrouteropener.domain.model.TlsInfo
import org.bouncycastle.tls.TlsClientProtocol
import java.io.BufferedInputStream
import java.io.BufferedOutputStream
import java.io.FilterInputStream
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
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
    private val origin: String,
    private val dnsResolver: (String) -> Array<InetAddress> = InetAddress::getAllByName
) : RouterHttpClient {
    private data class Attempt(val endpoint: Endpoint, val profile: TlsProfile?)
    private class BeforeWrite(val failure: IOException, val connected: Boolean, val plaintext: Boolean) : IOException()
    private val slots = Semaphore(preset.sockets, true)
    @Volatile private var preferred: Attempt? = null

    override fun send(request: RouterRequest, cookies: String): UpstreamResult {
        var acquired = acquireSlot()
        try {
            val cached = preferred
            val attempts = buildAttempts(cached)
            val dns = dnsResolver(target.host).filterNot { it.isAnyLocalAddress || it.isMulticastAddress }
            if (dns.isEmpty()) throw IOException("Target resolves to a blocked address")
            var last: IOException? = null
            var retriedCached = false
            while (attempts.isNotEmpty() && !sockets.closed) {
                val attempt = attempts.removeFirst()
                try {
                    return connect(attempt, dns, request, cookies).also { preferred = attempt }
                } catch (failure: BeforeWrite) {
                    last = failure.failure
                    val endpoint = attempt.endpoint
                    if (attempt == cached && !retriedCached &&
                        (last is SocketException || last is java.io.EOFException)
                    ) {
                        retriedCached = true
                        // Release the permit during retry backoff so scarce 2-socket presets don't stall.
                        slots.release()
                        acquired = false
                        Thread.sleep(150)
                        if (sockets.closed) break
                        acquired = acquireSlot()
                        attempts.addFirst(attempt)
                        continue
                    }
                    enqueueHttpFallbackIfNeeded(endpoint, last, failure.plaintext, cached, attempts)
                    if (attempt == cached) preferred = null
                    if (!failure.connected && last is SocketTimeoutException) break
                }
            }
            throw last ?: IOException("Session closed")
        } finally {
            if (acquired) slots.release()
        }
    }

    private fun acquireSlot(): Boolean {
        if (sockets.closed || !slots.tryAcquire(15, TimeUnit.SECONDS)) throw IOException("Upstream busy")
        return true
    }

    private fun buildAttempts(cached: Attempt?): ArrayDeque<Attempt> {
        val attempts = ArrayDeque<Attempt>()
        if (cached != null) attempts.add(cached)
        val profiles = if (!target.tls) listOf(null) else if (preset.preferLegacyTls) {
            listOf(TlsProfile.LEGACY_STRIPPED, TlsProfile.MODERN, TlsProfile.SSLV3_ONLY)
        } else {
            listOf(TlsProfile.MODERN, TlsProfile.LEGACY_STRIPPED, TlsProfile.SSLV3_ONLY)
        }
        profiles.map { Attempt(target, it) }.filterNot { it == cached }.forEach(attempts::add)
        return attempts
    }

    private fun enqueueHttpFallbackIfNeeded(
        endpoint: Endpoint,
        last: IOException,
        plaintext: Boolean,
        cached: Attempt?,
        attempts: ArrayDeque<Attempt>
    ) {
        val implicitHttp = !target.explicitScheme && !target.explicitPort &&
            (last is java.net.ConnectException || last is SocketException || last is java.io.EOFException)
        val customHttp = !target.explicitScheme && target.explicitPort && plaintext
        if (endpoint.tls && (implicitHttp || customHttp)) {
            val fallback = Attempt(endpoint.copy(tls = false, port = if (implicitHttp) 80 else endpoint.port), null)
            if (cached != fallback && fallback !in attempts) attempts.addLast(fallback)
        }
    }

    private fun connect(
        attempt: Attempt,
        addresses: List<InetAddress>,
        request: RouterRequest,
        cookies: String
    ): UpstreamResult {
        val endpoint = attempt.endpoint
        var socket: Socket? = null
        var connected = false
        var startedWrite = false
        var inspection: HandshakeInput? = null
        try {
            socket = dialSocket(endpoint, addresses)
            connected = true
            val rawInput = BufferedInputStream(socket.getInputStream())
            val rawOutput = socket.getOutputStream()
            val tls = attempt.profile?.let { profile ->
                val probe = HandshakeInput(rawInput)
                inspection = probe
                negotiateTls(socket, endpoint, profile, probe, rawOutput)
            }
            val output = BufferedOutputStream(tls?.first?.outputStream ?: rawOutput, 8192)
            val input = BufferedInputStream(tls?.first?.inputStream ?: rawInput)
            val headers = buildUpstreamHeaders(endpoint, request, cookies)
            SocketDeadline(socket, 15_000).use { deadline ->
                deadline.check()
                startedWrite = true
                HttpWireCodec.writeHead(output, "${request.method} ${request.path} HTTP/1.1", headers, canonicalNames = true)
                request.body.writeTo(output)
                output.flush()
            }
            request.body.close()
            val response = readUpstreamResponse(socket, input, request.method)
            val telemetry = tls?.second ?: TlsInfo(endpoint.authority, "HTTP")
            return UpstreamResult(response, endpoint, telemetry)
        } catch (failure: IOException) {
            if (startedWrite || sockets.closed) throw failure
            throw BeforeWrite(failure, connected, inspection?.isPlaintext() == true)
        } finally {
            if (socket != null) {
                sockets.forget(socket)
                runCatching { socket.close() }
            }
        }
    }

    private fun dialSocket(endpoint: Endpoint, addresses: List<InetAddress>): Socket {
        // Use vetted numerical addresses for the dial. HTTP Host and TLS SNI still use the configured hostname.
        val allowed = addresses.filterNot { it.isLoopbackAddress && endpoint.port == blockedPort }
        if (allowed.isEmpty()) throw IOException("Self-proxy blocked")
        var lastError: IOException? = null
        for (address in allowed) {
            if (sockets.closed) throw IOException("Session closed")
            val candidate = sockets.track(Socket())
            try {
                candidate.connect(InetSocketAddress(address, endpoint.port), 10_000)
                return candidate
            } catch (failure: IOException) {
                sockets.forget(candidate)
                runCatching { candidate.close() }
                lastError = failure
            }
        }
        throw lastError ?: IOException("Unable to connect")
    }

    private fun negotiateTls(
        socket: Socket,
        endpoint: Endpoint,
        profile: TlsProfile,
        probe: HandshakeInput,
        rawOutput: OutputStream
    ): Pair<TlsClientProtocol, TlsInfo> {
        val protocol = TlsClientProtocol(probe, rawOutput)
        val client = LegacyRouterTlsClient(endpoint, profile)
        SocketDeadline(socket, 10_000).use { deadline ->
            deadline.check()
            protocol.connect(client)
        }
        return protocol to client.info()
    }

    private fun buildUpstreamHeaders(endpoint: Endpoint, request: RouterRequest, cookies: String): Headers {
        val headers = HttpWireCodec.forwardedHeaders(request.headers).toMutableMap()
        headers.keys.removeAll(strippedUpstreamHeaders)
        headers["host"] = listOf(endpoint.authority)
        headers["connection"] = listOf("close")
        headers["accept-encoding"] = listOf("identity")
        val incomingOrigin = headers.first("origin")
        if (incomingOrigin == null || incomingOrigin.equals(origin, ignoreCase = true)) {
            headers["origin"] = listOf(endpoint.url)
        }
        val incomingReferer = headers.first("referer")
        headers["referer"] = listOf(
            when {
                incomingReferer == null -> "${endpoint.url}/"
                incomingReferer.equals(origin, ignoreCase = true) -> "${endpoint.url}/"
                incomingReferer.startsWith("$origin/", ignoreCase = true) ->
                    endpoint.url + incomingReferer.substring(origin.length)
                else -> incomingReferer
            }
        )
        if (cookies.isNotEmpty()) headers["cookie"] = listOf(cookies)
        if (request.body.size > 0 || request.method in setOf("POST", "PUT", "PATCH") ||
            request.headers.containsKey("content-length") || request.headers.containsKey("transfer-encoding")) {
            headers["content-length"] = listOf(request.body.size.toString())
        }
        return headers
    }

    private fun readUpstreamResponse(socket: Socket, input: InputStream, method: String): RouterResponse {
        val head = SocketDeadline(socket, 15_000).use {
            it.check()
            HttpWireCodec.readResponse(input)
        }
        val keepAliveEmptyRedirect = head.status in setOf(301, 302, 303, 307, 308) &&
            head.headers.first("location") != null &&
            head.headers.first("content-length") == null &&
            !head.headers.containsKey("transfer-encoding") &&
            head.headers.first("connection")?.equals("close", ignoreCase = true) != true
        val body = if (HttpWireCodec.hasBody(method, head.status) && !keepAliveEmptyRedirect) {
            SocketDeadline(socket, 30_000).use {
                it.check()
                HttpWireCodec.body(input, head.headers, budget, closeDelimited = true)
            }
        } else WireBody(budget)
        return RouterResponse(head.status, head.headers, body)
    }

    private companion object {
        val strippedUpstreamHeaders = setOf(
            "host", "cookie", "if-modified-since", "if-none-match", "upgrade-insecure-requests"
        )
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
