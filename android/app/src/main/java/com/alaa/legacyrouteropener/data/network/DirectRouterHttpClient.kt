package com.alaa.legacyrouteropener.data.network

import android.util.Log
import com.alaa.legacyrouteropener.data.model.RouterRequest
import com.alaa.legacyrouteropener.data.model.RouterResponse
import org.bouncycastle.tls.TlsClientProtocol
import org.bouncycastle.tls.TlsException
import org.bouncycastle.tls.TlsNoCloseNotifyException
import java.io.BufferedInputStream
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream
import java.io.EOFException
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.net.ConnectException
import java.net.InetSocketAddress
import java.net.Socket
import java.net.SocketException
import java.net.SocketTimeoutException
import java.net.UnknownHostException
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Semaphore
import java.util.zip.GZIPInputStream
import java.util.zip.InflaterInputStream

class DirectRouterHttpClient : RouterHttpClient {

    companion object {
        private const val TAG = "DirectRouterHttp"
        private const val MAX_CONCURRENT_REQUESTS = 4
        private const val MAX_RETRIES = 2
        private const val RETRY_DELAY_MS = 200L
        private const val DEFAULT_HTTPS_PORT = 443
        private const val DEFAULT_HTTP_PORT = 80
        private const val CONNECT_TIMEOUT_MS = 10_000
        private const val SO_TIMEOUT_MS = 15_000

        private val STRIPPED_OUTGOING_HEADERS = setOf(
            "host", "referer", "origin", "accept", "accept-encoding",
            "connection", "cookie", "content-length", "content-type",
            "if-modified-since", "if-none-match"
        )
        private val PROXY_AUTHORITY_REGEX = Regex("""https?://(127\.0\.0\.1|localhost)(:\d+)?""", RegexOption.IGNORE_CASE)
    }

    private data class Endpoint(
        val host: String,
        val port: Int,
        val useTls: Boolean,
        val explicitPortOrScheme: Boolean = false
    ) {
        val scheme: String get() = if (useTls) "https" else "http"
        val authority: String
            get() {
                val isDefaultPort = (useTls && port == DEFAULT_HTTPS_PORT) || (!useTls && port == DEFAULT_HTTP_PORT)
                return if (isDefaultPort) host else "$host:$port"
            }
    }

    // Fair FIFO semaphore to prevent flooding the router's small MIPS CPU with parallel RSA TLS handshakes
    private val connectionSemaphore = Semaphore(MAX_CONCURRENT_REQUESTS, true)

    // Remembers hosts where default HTTPS (443) refused connection and fell back to plain HTTP (80)
    private val httpFallbackHosts = ConcurrentHashMap<String, Boolean>()

    override fun sendRequest(
        routerIp: String,
        request: RouterRequest,
        cookieHeader: String?
    ): RouterResponse {
        var endpoint = parseEndpoint(routerIp)
        var fellBackToHttp = false
        var lastError: Exception? = null

        for (attempt in 0..MAX_RETRIES) {
            try {
                connectionSemaphore.acquire()
                try {
                    val response = executeSingleRequest(endpoint, request, cookieHeader)
                    if (fellBackToHttp) {
                        httpFallbackHosts[endpoint.host] = true
                    }
                    return response
                } catch (e: Exception) {
                    if (shouldFallbackToHttp(endpoint, e)) {
                        Log.w(
                            TAG,
                            "🔄 HTTPS port $DEFAULT_HTTPS_PORT refused connection on ${endpoint.host}; falling back to plain HTTP on port $DEFAULT_HTTP_PORT"
                        )
                        fellBackToHttp = true
                        endpoint = Endpoint(
                            host = endpoint.host,
                            port = DEFAULT_HTTP_PORT,
                            useTls = false,
                            explicitPortOrScheme = true
                        )
                        val response = executeSingleRequest(endpoint, request, cookieHeader)
                        httpFallbackHosts[endpoint.host] = true
                        return response
                    }
                    if (!endpoint.explicitPortOrScheme &&
                        !endpoint.useTls &&
                        endpoint.port == DEFAULT_HTTP_PORT &&
                        isConnectionRefused(e)
                    ) {
                        httpFallbackHosts.remove(endpoint.host)
                        Log.w(
                            TAG,
                            "🔄 Cached HTTP port $DEFAULT_HTTP_PORT refused connection on ${endpoint.host}; retrying HTTPS on port $DEFAULT_HTTPS_PORT"
                        )
                        endpoint = Endpoint(
                            host = endpoint.host,
                            port = DEFAULT_HTTPS_PORT,
                            useTls = true,
                            explicitPortOrScheme = false
                        )
                        return executeSingleRequest(endpoint, request, cookieHeader)
                    }
                    throw e
                } finally {
                    connectionSemaphore.release()
                }
            } catch (e: InterruptedException) {
                Thread.currentThread().interrupt()
                throw IOException("Interrupted while waiting for router connection slot", e)
            } catch (e: Exception) {
                lastError = e
                if (attempt < MAX_RETRIES && isRetryable(e)) {
                    Log.w(
                        TAG,
                        "⚠️ Retry ${attempt + 1}/$MAX_RETRIES for ${request.method} ${request.path} after ${e.javaClass.simpleName}: ${e.message}"
                    )
                    try {
                        Thread.sleep(RETRY_DELAY_MS)
                    } catch (ie: InterruptedException) {
                        Thread.currentThread().interrupt()
                        throw e
                    }
                    continue
                }
                logFailure(e)
                throw e
            }
        }

        val finalError = lastError ?: IOException("Request failed")
        logFailure(finalError)
        throw finalError
    }

    private fun parseEndpoint(routerIp: String): Endpoint {
        val raw = routerIp.trim()
        val hasHttpScheme = raw.startsWith("http://", ignoreCase = true)
        val hasHttpsScheme = raw.startsWith("https://", ignoreCase = true)
        val cleaned = raw
            .replace(Regex("^https?://", RegexOption.IGNORE_CASE), "")
            .substringBefore('/')
            .trim()

        val colonIdx = cleaned.lastIndexOf(':')
        var host = cleaned
        var port: Int? = null
        if (colonIdx > 0 && !cleaned.startsWith("[")) {
            val parsedPort = cleaned.substring(colonIdx + 1).trim().toIntOrNull()
            if (parsedPort != null && parsedPort in 1..65535) {
                host = cleaned.substring(0, colonIdx).trim()
                port = parsedPort
            }
        }

        val finalHost = host.ifEmpty { "192.168.1.1" }
        val explicitPortOrScheme = hasHttpScheme || hasHttpsScheme || port != null

        if (!explicitPortOrScheme && httpFallbackHosts[finalHost] == true) {
            return Endpoint(
                host = finalHost,
                port = DEFAULT_HTTP_PORT,
                useTls = false,
                explicitPortOrScheme = false
            )
        }

        val finalPort = port ?: if (hasHttpScheme) DEFAULT_HTTP_PORT else DEFAULT_HTTPS_PORT
        val useTls = when {
            hasHttpScheme -> false
            hasHttpsScheme -> true
            finalPort == DEFAULT_HTTP_PORT -> false
            else -> true
        }
        return Endpoint(finalHost, finalPort, useTls, explicitPortOrScheme)
    }

    private fun isConnectionRefused(error: Throwable): Boolean {
        return generateSequence(error) { it.cause }.any { t ->
            t is ConnectException ||
                (t is SocketException && (
                    t.message?.contains("Connection refused", ignoreCase = true) == true ||
                        t.message?.contains("ECONNREFUSED", ignoreCase = true) == true
                    ))
        }
    }

    private fun shouldFallbackToHttp(endpoint: Endpoint, error: Throwable): Boolean {
        if (endpoint.explicitPortOrScheme || !endpoint.useTls || endpoint.port != DEFAULT_HTTPS_PORT) {
            return false
        }
        return isConnectionRefused(error)
    }

    private fun isRetryable(error: Throwable): Boolean {
        if (isConnectionRefused(error)) return false

        return generateSequence(error) { it.cause }.any { t ->
            t is SocketException ||
                t is SocketTimeoutException ||
                t is EOFException ||
                t is TlsException ||
                (t is IOException && t !is UnknownHostException && (
                    t.message?.contains("reset", ignoreCase = true) == true ||
                        t.message?.contains("eof", ignoreCase = true) == true ||
                        t.message?.contains("timed out", ignoreCase = true) == true ||
                        t.message?.contains("closed", ignoreCase = true) == true ||
                        t.message?.contains("handshake", ignoreCase = true) == true
                    ))
        }
    }

    private fun logFailure(e: Exception) {
        Log.e(TAG, "!!! TLS/HTTP FAILED: ${e.javaClass.simpleName}: ${e.message}")
        Log.e(TAG, "!!! Full stack trace:", e)

        var cause = e.cause
        var depth = 1
        while (cause != null) {
            Log.e(TAG, "!!! Cause [$depth]: ${cause.javaClass.simpleName}: ${cause.message}")
            cause = cause.cause
            depth++
        }
    }

    private fun executeSingleRequest(
        endpoint: Endpoint,
        request: RouterRequest,
        cookieHeader: String?
    ): RouterResponse {
        var rawSocket: Socket? = null
        var tlsProtocol: TlsClientProtocol? = null

        try {
            Log.d(TAG, "=== START REQUEST: ${request.method} ${request.path} ===")
            Log.d(TAG, "Step 1: Creating TCP socket to ${endpoint.host}:${endpoint.port} (TLS=${endpoint.useTls})")

            rawSocket = Socket()
            rawSocket.tcpNoDelay = true
            rawSocket.soTimeout = SO_TIMEOUT_MS
            rawSocket.connect(InetSocketAddress(endpoint.host, endpoint.port), CONNECT_TIMEOUT_MS)

            Log.d(TAG, "Step 2: TCP connected. Local=${rawSocket.localPort}, Remote=${rawSocket.remoteSocketAddress}")

            val out: OutputStream
            val rawIn: InputStream
            if (endpoint.useTls) {
                Log.d(TAG, "Step 3: Creating TlsClientProtocol")
                tlsProtocol = TlsClientProtocol(rawSocket.getInputStream(), rawSocket.getOutputStream())

                Log.d(TAG, "Step 4: Creating LegacyRouterTlsClient")
                val tlsClient = LegacyRouterTlsClient()

                Log.d(TAG, "Step 5: Starting TLS handshake (tlsProtocol.connect)...")
                tlsProtocol.connect(tlsClient)
                Log.i(TAG, "Step 6: TLS handshake SUCCESS!")

                out = tlsProtocol.outputStream
                rawIn = tlsProtocol.inputStream
            } else {
                Log.d(TAG, "Step 3-6: Using plain HTTP connection on port ${endpoint.port}")
                out = rawSocket.getOutputStream()
                rawIn = rawSocket.getInputStream()
            }

            val schemeAuthority = "${endpoint.scheme}://${endpoint.authority}"
            val reqHeader = StringBuilder()
            reqHeader.append("${request.method} ${request.path} HTTP/1.1\r\n")
            reqHeader.append("Host: ${endpoint.authority}\r\n")

            val incomingReferer = request.headers.entries
                .firstOrNull { it.key.equals("referer", ignoreCase = true) }
                ?.value
            val outgoingReferer = if (!incomingReferer.isNullOrBlank()) {
                PROXY_AUTHORITY_REGEX.replace(incomingReferer, schemeAuthority)
            } else {
                "$schemeAuthority/"
            }
            reqHeader.append("Referer: $outgoingReferer\r\n")
            reqHeader.append("Origin: $schemeAuthority\r\n")
            reqHeader.append("Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8\r\n")
            reqHeader.append("Accept-Encoding: identity\r\n")
            reqHeader.append("Connection: close\r\n")

            for ((k, v) in request.headers) {
                if (k.lowercase() !in STRIPPED_OUTGOING_HEADERS) {
                    reqHeader.append("$k: $v\r\n")
                }
            }

            if (!cookieHeader.isNullOrBlank()) {
                reqHeader.append("Cookie: $cookieHeader\r\n")
            }

            val reqBody = request.body
            if (reqBody != null && reqBody.isNotEmpty()) {
                val contentType = request.headers.entries
                    .firstOrNull { it.key.equals("content-type", ignoreCase = true) }
                    ?.value ?: "application/x-www-form-urlencoded"
                reqHeader.append("Content-Type: $contentType\r\n")
                reqHeader.append("Content-Length: ${reqBody.size}\r\n")
            } else if (request.method.equals("POST", ignoreCase = true) || request.method.equals("PUT", ignoreCase = true)) {
                reqHeader.append("Content-Length: 0\r\n")
            }

            reqHeader.append("\r\n")

            Log.d(TAG, "Step 7: Sending HTTP request:\n${reqHeader}")
            out.write(reqHeader.toString().toByteArray(Charsets.ISO_8859_1))

            if (reqBody != null && reqBody.isNotEmpty()) {
                out.write(reqBody)
                Log.d(TAG, "Step 7b: Sent ${reqBody.size} bytes of body")
            }
            out.flush()

            Log.d(TAG, "Step 8: Reading HTTP response...")
            val inStream = BufferedInputStream(rawIn)
            val response = parseResponse(inStream, request.method)
            Log.i(TAG, "Step 9: Got response: ${response.statusCode} ${response.statusText} (${response.body.size} bytes)")
            val contentType = response.headers.entries.firstOrNull { it.key.equals("Content-Type", ignoreCase = true) }?.value?.firstOrNull() ?: ""
            if (request.path.endsWith(".asp", ignoreCase = true) ||
                request.path.endsWith(".html", ignoreCase = true) ||
                request.path.endsWith(".cgi", ignoreCase = true) ||
                contentType.contains("html", ignoreCase = true)
            ) {
                val preview = try {
                    val text = String(response.body, Charsets.UTF_8).replace(Regex("\\s+"), " ").trim()
                    if (text.length > 200) text.take(200) + "..." else text
                } catch (_: Exception) { "" }
                if (preview.isNotEmpty()) {
                    Log.d(TAG, "📄 [HTML Preview] $preview")
                }
            }
            Log.d(TAG, "=== END REQUEST: ${request.method} ${request.path} ===")
            return response
        } finally {
            try { tlsProtocol?.close() } catch (_: Exception) {}
            try { rawSocket?.close() } catch (_: Exception) {}
        }
    }

    /**
     * Safely read a single byte, treating TlsNoCloseNotifyException and any stream close/reset as EOF (-1).
     * Legacy routers close TCP without sending TLS close_notify — this is normal.
     */
    private fun safeRead(inStream: InputStream): Int {
        return try {
            inStream.read()
        } catch (e: TlsNoCloseNotifyException) {
            Log.d(TAG, "Router closed connection without close_notify (normal for legacy firmware)")
            -1
        } catch (e: Exception) {
            Log.d(TAG, "Read stream ended: ${e.javaClass.simpleName}: ${e.message}")
            -1
        }
    }

    private fun safeRead(inStream: InputStream, buf: ByteArray, off: Int, len: Int): Int {
        return try {
            inStream.read(buf, off, len)
        } catch (e: TlsNoCloseNotifyException) {
            Log.d(TAG, "Router closed connection without close_notify (normal for legacy firmware)")
            -1
        } catch (e: Exception) {
            Log.d(TAG, "Read stream ended: ${e.javaClass.simpleName}: ${e.message}")
            -1
        }
    }

    private fun safeReadBytes(inStream: InputStream): ByteArray {
        return try {
            val out = ByteArrayOutputStream()
            val buf = ByteArray(4096)
            while (true) {
                val n = safeRead(inStream, buf, 0, buf.size)
                if (n == -1) break
                out.write(buf, 0, n)
            }
            out.toByteArray()
        } catch (e: Exception) {
            Log.d(TAG, "safeReadBytes ended: ${e.javaClass.simpleName}: ${e.message}")
            ByteArray(0)
        }
    }

    private fun parseResponse(inStream: InputStream, requestMethod: String): RouterResponse {
        val headerBytes = ByteArrayOutputStream()
        var b3 = -1
        var b2 = -1
        var b1 = -1

        while (true) {
            val b = try {
                inStream.read()
            } catch (e: TlsNoCloseNotifyException) {
                Log.d(TAG, "Router closed connection without close_notify while reading headers")
                -1
            }
            if (b == -1) break
            headerBytes.write(b)

            if ((b3 == '\r'.code && b2 == '\n'.code && b1 == '\r'.code && b == '\n'.code) ||
                (b1 == '\n'.code && b == '\n'.code)
            ) {
                break
            }
            b3 = b2
            b2 = b1
            b1 = b
        }

        if (headerBytes.size() == 0) {
            throw EOFException("Router closed connection before sending HTTP headers")
        }

        val headerText = headerBytes.toString("ISO-8859-1")
        val lines = headerText.split(Regex("\r?\n")).filter { it.isNotBlank() }

        val statusLine = lines.firstOrNull()
            ?: throw EOFException("Router returned empty HTTP status line")
        val statusParts = statusLine.split(" ", limit = 3)
        val statusCode = statusParts.getOrNull(1)?.toIntOrNull() ?: 200
        val statusText = if (statusParts.size > 2) statusParts[2] else "OK"

        val headers = mutableMapOf<String, MutableList<String>>()
        for (i in 1 until lines.size) {
            val line = lines[i]
            val colonIdx = line.indexOf(':')
            if (colonIdx > 0) {
                val key = line.substring(0, colonIdx).trim()
                val value = line.substring(colonIdx + 1).trim()
                headers.getOrPut(key) { mutableListOf() }.add(value)
            }
        }

        val transferEncoding = headers.entries.firstOrNull {
            it.key.equals("Transfer-Encoding", ignoreCase = true)
        }?.value?.firstOrNull()
        val contentLength = headers.entries.firstOrNull {
            it.key.equals("Content-Length", ignoreCase = true)
        }?.value?.firstOrNull()?.trim()?.toIntOrNull()
        val contentEncoding = headers.entries.firstOrNull {
            it.key.equals("Content-Encoding", ignoreCase = true)
        }?.value?.firstOrNull()?.trim()?.lowercase()

        val noBodyStatus = statusCode in 100..199 || statusCode == 204 || statusCode == 304
        val rawBodyBytes: ByteArray = when {
            noBodyStatus || requestMethod.equals("HEAD", ignoreCase = true) -> ByteArray(0)
            transferEncoding?.contains("chunked", ignoreCase = true) == true -> readChunkedBody(inStream)
            contentLength != null -> {
                if (contentLength <= 0) {
                    ByteArray(0)
                } else {
                    val buf = ByteArray(contentLength)
                    var totalRead = 0
                    while (totalRead < contentLength) {
                        val read = safeRead(inStream, buf, totalRead, contentLength - totalRead)
                        if (read == -1) break
                        totalRead += read
                    }
                    if (totalRead == contentLength) buf else buf.copyOf(totalRead)
                }
            }
            else -> safeReadBytes(inStream)
        }

        val bodyBytes = if (rawBodyBytes.isNotEmpty()) {
            decompressIfNeeded(rawBodyBytes, contentEncoding)
        } else {
            rawBodyBytes
        }

        return RouterResponse(statusCode, statusText, headers, bodyBytes)
    }

    private fun decompressIfNeeded(bytes: ByteArray, encoding: String?): ByteArray {
        if (encoding.isNullOrBlank()) return bytes
        return try {
            when {
                encoding.contains("gzip") -> GZIPInputStream(ByteArrayInputStream(bytes)).use { it.readBytes() }
                encoding.contains("deflate") -> InflaterInputStream(ByteArrayInputStream(bytes)).use { it.readBytes() }
                else -> bytes
            }
        } catch (e: Exception) {
            Log.w(TAG, "Failed to decompress $encoding body (${e.message}), returning raw bytes")
            bytes
        }
    }

    private fun readChunkedBody(inStream: InputStream): ByteArray {
        val out = ByteArrayOutputStream()
        while (true) {
            val sizeLine = readLineOrNull(inStream)?.trim() ?: break
            if (sizeLine.isEmpty()) continue
            val chunkSize = try {
                sizeLine.split(";")[0].trim().toInt(16)
            } catch (_: NumberFormatException) {
                break
            }
            if (chunkSize == 0) {
                while (true) {
                    val trailer = readLineOrNull(inStream) ?: break
                    if (trailer.isEmpty()) break
                }
                break
            }
            val chunk = ByteArray(chunkSize)
            var readTotal = 0
            while (readTotal < chunkSize) {
                val r = safeRead(inStream, chunk, readTotal, chunkSize - readTotal)
                if (r == -1) break
                readTotal += r
            }
            out.write(chunk, 0, readTotal)
            readLineOrNull(inStream)
        }
        return out.toByteArray()
    }

    private fun readLineOrNull(inStream: InputStream): String? {
        val out = ByteArrayOutputStream()
        var sawAnyByte = false
        while (true) {
            val b = safeRead(inStream)
            if (b == -1) {
                return if (sawAnyByte) out.toString("ISO-8859-1") else null
            }
            sawAnyByte = true
            if (b == '\n'.code) break
            if (b != '\r'.code) out.write(b)
        }
        return out.toString("ISO-8859-1")
    }
}
