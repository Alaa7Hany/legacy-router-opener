package com.alaa.legacyrouteropener.data.network

import android.util.Log
import com.alaa.legacyrouteropener.data.model.RouterRequest
import com.alaa.legacyrouteropener.data.model.RouterResponse
import org.bouncycastle.tls.TlsClientProtocol
import java.io.BufferedInputStream
import java.io.ByteArrayOutputStream
import java.io.InputStream
import java.net.InetSocketAddress
import java.net.Socket

class DirectRouterHttpClient : RouterHttpClient {

    companion object {
        private const val TAG = "DirectRouterHttp"
    }

    override fun sendRequest(
        routerIp: String,
        request: RouterRequest,
        cookieHeader: String?
    ): RouterResponse {
        var rawSocket: Socket? = null
        var tlsProtocol: TlsClientProtocol? = null

        try {
            Log.d(TAG, "=== START REQUEST: ${request.method} ${request.path} ===")
            Log.d(TAG, "Step 1: Creating TCP socket to $routerIp:443")

            rawSocket = Socket()
            rawSocket.tcpNoDelay = true
            rawSocket.soTimeout = 15000
            rawSocket.connect(InetSocketAddress(routerIp, 443), 10000)

            Log.d(TAG, "Step 2: TCP connected. Local=${rawSocket.localPort}, Remote=${rawSocket.remoteSocketAddress}")
            Log.d(TAG, "Step 3: Creating TlsClientProtocol")

            tlsProtocol = TlsClientProtocol(rawSocket.getInputStream(), rawSocket.getOutputStream())

            Log.d(TAG, "Step 4: Creating LegacyRouterTlsClient")
            val tlsClient = LegacyRouterTlsClient()

            Log.d(TAG, "Step 5: Starting TLS handshake (tlsProtocol.connect)...")
            tlsProtocol.connect(tlsClient)
            Log.i(TAG, "Step 6: TLS handshake SUCCESS!")

            val out = tlsProtocol.outputStream
            val reqHeader = StringBuilder()
            reqHeader.append("${request.method} ${request.path} HTTP/1.1\r\n")
            reqHeader.append("Host: $routerIp\r\n")

            val incomingReferer = request.headers["referer"]
            val outgoingReferer = if (!incomingReferer.isNullOrBlank()) {
                val proxyRegex = Regex("""https?://(127\.0\.0\.1|localhost)(:\d+)?""", RegexOption.IGNORE_CASE)
                proxyRegex.replace(incomingReferer, "https://$routerIp")
            } else {
                "https://$routerIp/"
            }
            reqHeader.append("Referer: $outgoingReferer\r\n")
            reqHeader.append("Origin: https://$routerIp\r\n")
            reqHeader.append("Accept: text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8\r\n")
            reqHeader.append("Accept-Encoding: identity\r\n")
            reqHeader.append("Connection: close\r\n")

            for ((k, v) in request.headers) {
                val lk = k.lowercase()
                if (lk !in listOf(
                        "host", "referer", "origin", "accept", "accept-encoding",
                        "connection", "cookie", "content-length", "content-type"
                    )
                ) {
                    reqHeader.append("$k: $v\r\n")
                }
            }

            if (!cookieHeader.isNullOrBlank()) {
                reqHeader.append("Cookie: $cookieHeader\r\n")
            }

            if (request.body != null && request.body.isNotEmpty()) {
                // NanoHTTPD strips Content-Type; re-add it for POST/PUT requests
                val contentType = request.headers["content-type"] ?: "application/x-www-form-urlencoded"
                reqHeader.append("Content-Type: $contentType\r\n")
                reqHeader.append("Content-Length: ${request.body.size}\r\n")
            }

            reqHeader.append("\r\n")

            Log.d(TAG, "Step 7: Sending HTTP request over TLS:\n${reqHeader}")
            out.write(reqHeader.toString().toByteArray(Charsets.ISO_8859_1))

            if (request.body != null && request.body.isNotEmpty()) {
                out.write(request.body)
                Log.d(TAG, "Step 7b: Sent ${request.body.size} bytes of body")
            }
            out.flush()

            Log.d(TAG, "Step 8: Reading HTTP response...")
            val inStream = BufferedInputStream(tlsProtocol.inputStream)
            val response = parseResponse(inStream)
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

        } catch (e: Exception) {
            Log.e(TAG, "!!! TLS/HTTP FAILED: ${e.javaClass.simpleName}: ${e.message}")
            Log.e(TAG, "!!! Full stack trace:", e)

            // Also log the cause chain
            var cause = e.cause
            var depth = 1
            while (cause != null) {
                Log.e(TAG, "!!! Cause [$depth]: ${cause.javaClass.simpleName}: ${cause.message}")
                cause = cause.cause
                depth++
            }

            throw e
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
        } catch (e: org.bouncycastle.tls.TlsNoCloseNotifyException) {
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
        } catch (e: org.bouncycastle.tls.TlsNoCloseNotifyException) {
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

    private fun parseResponse(inStream: InputStream): RouterResponse {
        val headerBytes = ByteArrayOutputStream()
        var lastChar = -1
        var crlfCount = 0

        while (true) {
            val b = safeRead(inStream)
            if (b == -1) break
            headerBytes.write(b)

            if (b == '\n'.code && lastChar == '\r'.code) {
                crlfCount++
                if (crlfCount == 2) break
            } else if (b != '\r'.code && b != '\n'.code) {
                crlfCount = 0
            }
            lastChar = b
        }

        val headerText = headerBytes.toString("ISO-8859-1")
        val lines = headerText.split("\r\n").filter { it.isNotBlank() }

        val statusLine = lines.firstOrNull() ?: "HTTP/1.1 200 OK"
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
        }?.value?.firstOrNull()?.toIntOrNull()

        val bodyBytes: ByteArray = if (transferEncoding?.contains("chunked", ignoreCase = true) == true) {
            readChunkedBody(inStream)
        } else if (contentLength != null && contentLength > 0) {
            val buf = ByteArray(contentLength)
            var totalRead = 0
            while (totalRead < contentLength) {
                val read = safeRead(inStream, buf, totalRead, contentLength - totalRead)
                if (read == -1) break
                totalRead += read
            }
            if (totalRead == contentLength) buf else buf.copyOf(totalRead)
        } else {
            safeReadBytes(inStream)
        }

        return RouterResponse(statusCode, statusText, headers, bodyBytes)
    }

    private fun readChunkedBody(inStream: InputStream): ByteArray {
        val out = ByteArrayOutputStream()
        while (true) {
            val sizeLine = readLine(inStream).trim()
            if (sizeLine.isEmpty()) continue
            val chunkSize = try {
                sizeLine.split(";")[0].trim().toInt(16)
            } catch (e: NumberFormatException) {
                break
            }
            if (chunkSize == 0) {
                readLine(inStream)
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
            readLine(inStream)
        }
        return out.toByteArray()
    }

    private fun readLine(inStream: InputStream): String {
        val out = ByteArrayOutputStream()
        while (true) {
            val b = safeRead(inStream)
            if (b == -1 || b == '\n'.code) break
            if (b != '\r'.code) out.write(b)
        }
        return out.toString("ISO-8859-1")
    }
}
