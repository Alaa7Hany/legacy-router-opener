package com.alaa.legacyrouteropener.data.proxy

import com.alaa.legacyrouteropener.domain.model.AppLanguage
import com.alaa.legacyrouteropener.domain.model.Endpoint
import com.alaa.legacyrouteropener.domain.model.HardwarePreset
import com.alaa.legacyrouteropener.data.network.RouterRequest
import com.alaa.legacyrouteropener.data.network.RouterResponse
import com.alaa.legacyrouteropener.domain.model.TlsInfo
import com.alaa.legacyrouteropener.domain.model.redactQuery
import com.alaa.legacyrouteropener.data.network.*
import com.alaa.legacyrouteropener.data.repository.SessionCookieJar
import java.io.Closeable
import java.io.IOException

internal class ProxyServer(
    private val endpoint: Endpoint,
    private val authority: String,
    private val preset: HardwarePreset,
    private val budget: MemoryBudget,
    private val upstream: RouterHttpClient,
    private val language: () -> AppLanguage,
    message: (AppLanguage, String) -> String,
    private val telemetry: (TlsInfo) -> Unit,
    private val log: (String) -> Unit
) : Closeable {
    private val jar = SessionCookieJar(preset.seedCookies)
    private val cache = StaticCache()
    private val errorPageRenderer = ErrorPageRenderer(message)
    private val browserCookies = linkedSetOf<Pair<String, String>>()

    fun retiredCookies(): List<Pair<String, String>> =
        synchronized(browserCookies) { browserCookies.toList() }

    override fun close() {
        cache.clear()
    }

    fun serve(request: RouterRequest): RouterResponse {
        val safePath = redactQuery(request.path)
        log("[${request.method}] $safePath")
        val incomingCookies = request.headers["cookie"].orEmpty()
        val cacheableRequest = request.method == "GET" && ResponseTransformer.isStatic(request.path) &&
            request.headers.keys.none { it in requestCacheExclusions }
        return try {
            if (cacheableRequest) {
                cache.get(request.path, budget)?.let {
                    log("[CACHE] $safePath")
                    return it
                }
            }
            val result = upstream.send(request, jar.merge(incomingCookies))
            telemetry(result.tlsInfo)
            result.response.use { response ->
                processUpstreamResponse(request, safePath, cacheableRequest, result.endpoint, response)
            }
        } catch (_: InterruptedException) {
            Thread.currentThread().interrupt()
            badGateway()
        } catch (failure: IOException) {
            log("[ERR] $safePath (${failure.javaClass.simpleName})")
            badGateway()
        } catch (failure: IllegalArgumentException) {
            log("[ERR] $safePath (${failure.javaClass.simpleName})")
            badGateway()
        }
    }

    private fun processUpstreamResponse(
        request: RouterRequest,
        safePath: String,
        cacheableRequest: Boolean,
        used: Endpoint,
        response: RouterResponse
    ): RouterResponse {
        val responseHeaders = HttpWireCodec.forwardedHeaders(response.headers).toMutableMap()
        val cookies = responseHeaders["set-cookie"].orEmpty()
        if (cookies.isNotEmpty()) {
            jar.save(cookies)
            val cleaned = cookies.map(ResponseTransformer::cleanCookie).filter(String::isNotEmpty)
            recordBrowserCookies(request.path, cleaned)
            responseHeaders["set-cookie"] = cleaned
        }
        responseHeaders.keys.removeAll(stripResponseHeaders)
        val transformer = ResponseTransformer(used, authority)
        responseHeaders["location"]?.let { values ->
            responseHeaders["location"] = values.map { transformer.redirect(it, preset.rewriteSingleLabelHost) }
        }
        val mime = ResponseTransformer.mimeType(request.path, responseHeaders.first("content-type"))
        responseHeaders["content-type"] = listOf(mime)
        val encoding = responseHeaders.first("content-encoding")
        val hasBody = HttpWireCodec.hasBody(request.method, response.status)
        val decode = hasBody && response.body.size > 0 && encoding != null &&
            !encoding.equals("identity", ignoreCase = true) &&
            encoding.lowercase() in setOf("gzip", "x-gzip", "deflate")
        val decoded = if (decode) HttpWireCodec.decode(response.body, encoding!!, budget) else response.body
        var transformed = decoded
        try {
            if (decode) {
                response.body.close()
                responseHeaders.remove("content-encoding")
            }
            if (hasBody && (decode || encoding == null || encoding.equals("identity", ignoreCase = true))) {
                transformed = transformer.transform(decoded, mime, budget)
            }
            if (decode || transformed !== decoded) {
                responseHeaders.keys.removeAll(setOf("etag", "content-md5", "digest", "content-range", "accept-ranges"))
            }
            responseHeaders["cache-control"] = listOf("no-store")
            responseHeaders["referrer-policy"] = listOf("same-origin")
            if ((!hasBody || request.method == "HEAD") && response.headers.first("content-length") != null) {
                responseHeaders["content-length"] = response.headers["content-length"]!!
            }
            maybeCache(cacheableRequest, request.path, response, cookies, responseHeaders, transformed)
            log("[${response.status}] $safePath")
            // The caller owns this response body; transfer slabs without buffering another copy.
            return RouterResponse(response.status, responseHeaders, transformed.takeFrom())
        } finally {
            if (transformed !== decoded) transformed.close()
            if (decoded !== response.body) decoded.close()
        }
    }

    private fun maybeCache(
        cacheableRequest: Boolean,
        path: String,
        response: RouterResponse,
        cookies: List<String>,
        responseHeaders: Headers,
        transformed: WireBody
    ) {
        val forbidden = response.headers.keys.any { it in responseCacheExclusions } ||
            directives(response.headers).any { it in uncacheableDirectives }
        val isPublicAsset = cacheableRequest && response.status == 200 && !forbidden && cookies.isEmpty()
        if (isPublicAsset && transformed.size <= 512 * 1024) {
            cache.put(path, responseHeaders, transformed, budget)
        }
    }

    private fun badGateway(): RouterResponse {
        val page = errorPageRenderer.render(language(), endpoint.authority)
        val body = WireBody(budget)
        try {
            body.write(page)
        } catch (_: BudgetExceeded) {
            body.close()
        }
        return RouterResponse(502, mapOf(
            "content-type" to listOf("text/html; charset=utf-8"),
            "cache-control" to listOf("no-store"),
            "content-security-policy" to listOf("default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'"),
            "referrer-policy" to listOf("no-referrer")
        ), body)
    }

    private fun directives(headers: Headers) = (headers["cache-control"].orEmpty() + headers["pragma"].orEmpty())
        .flatMap { it.lowercase().split(',') }.map { it.trim().substringBefore('=') }

    private fun recordBrowserCookies(path: String, headers: List<String>) = synchronized(browserCookies) {
        val defaultPath = path.substringBefore('?').substringBeforeLast('/').ifEmpty { "/" }
        for (header in headers) {
            val parts = header.split(';').map(String::trim)
            val name = parts.first().substringBefore('=')
            if (!cookieName.matches(name)) continue
            val cookiePath = parts.drop(1).firstOrNull { it.startsWith("Path=", ignoreCase = true) }
                ?.substringAfter('=')?.takeIf { it.startsWith('/') } ?: defaultPath
            if (browserCookies.size == 256) browserCookies.remove(browserCookies.first())
            browserCookies.add(name to cookiePath)
        }
    }

    private class StaticCache {
        private data class Asset(val headers: Headers, val payload: ByteArray, val reservation: Closeable)
        private val entries = LinkedHashMap<String, Asset>(16, .75f, true)
        private var size = 0

        @Synchronized
        fun get(path: String, budget: MemoryBudget): RouterResponse? {
            val asset = entries[path] ?: return null
            val body = WireBody(budget)
            try {
                body.write(asset.payload)
                return RouterResponse(200, asset.headers, body)
            } catch (_: BudgetExceeded) {
                body.close()
                return null
            }
        }

        @Synchronized
        fun put(path: String, headers: Headers, body: WireBody, budget: MemoryBudget) {
            val byteSize = body.size
            val reservation = try {
                budget.reserve(byteSize)
            } catch (_: BudgetExceeded) {
                return
            }
            try {
                val payload = ByteArray(byteSize)
                body.inputStream().use { input ->
                    var offset = 0
                    while (offset < payload.size) {
                        val read = input.read(payload, offset, payload.size - offset)
                        if (read < 0) break
                        offset += read
                    }
                    if (offset != payload.size) {
                        reservation.close()
                        return
                    }
                }
                entries.remove(path)?.let { existing ->
                    size -= existing.payload.size
                    existing.reservation.close()
                }
                while (entries.size >= 128 || size + byteSize > 8 * MIB) {
                    val first = entries.entries.iterator()
                    val evicted = first.next().value
                    size -= evicted.payload.size
                    evicted.reservation.close()
                    first.remove()
                }
                entries[path] = Asset(headers, payload, reservation)
                size += byteSize
            } catch (failure: Throwable) {
                reservation.close()
                throw failure
            }
        }

        @Synchronized
        fun clear() {
            entries.values.forEach { it.reservation.close() }
            entries.clear()
            size = 0
        }
    }

    private companion object {
        val cookieName = Regex("[A-Za-z0-9_!#$%&'*+.^|~-]+")
        val requestCacheExclusions = setOf("authorization", "cache-control", "pragma", "range",
            "if-none-match", "if-modified-since", "if-match", "if-unmodified-since")
        val responseCacheExclusions = setOf("vary", "www-authenticate", "content-range")
        val uncacheableDirectives = setOf("no-store", "no-cache", "private")
        val stripResponseHeaders = setOf(
            "strict-transport-security", "alt-svc", "clear-site-data",
            "access-control-allow-origin", "access-control-allow-credentials",
            "x-frame-options",
            "content-security-policy", "content-security-policy-report-only",
            "x-content-security-policy", "x-webkit-csp",
            "cross-origin-embedder-policy", "cross-origin-opener-policy",
            "cross-origin-resource-policy", "x-content-type-options"
        )
    }
}
