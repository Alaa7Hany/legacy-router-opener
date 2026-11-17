package com.alaa.legacyrouteropener.domain.server

import com.alaa.legacyrouteropener.data.model.RouterRequest
import com.alaa.legacyrouteropener.data.network.RouterHttpClient
import com.alaa.legacyrouteropener.data.repository.SessionCookieJar
import fi.iki.elonen.NanoHTTPD
import java.io.ByteArrayInputStream
import java.io.InputStream
import java.io.PrintWriter
import java.util.HashMap
import java.util.concurrent.ConcurrentHashMap

class ProxyServer(
    val proxyHost: String = "127.0.0.1",
    val proxyPort: Int = 8080,
    routerIp: String = "192.168.1.1",
    private val httpClient: RouterHttpClient,
    val cookieJar: SessionCookieJar = SessionCookieJar(),
    private val onLog: ((String) -> Unit)? = null
) : NanoHTTPD(proxyHost, proxyPort) {

    companion object {
        private val STATIC_ASSET_EXTENSIONS = setOf(
            ".gif", ".png", ".jpg", ".jpeg", ".ico", ".svg", ".css", ".js", ".res"
        )
        private const val MAX_CACHED_ASSETS = 128
        private const val MAX_CACHED_ASSET_BYTES = 512 * 1024

        private val STRIPPED_HEADERS = setOf(
            "content-length", "content-encoding", "set-cookie", "transfer-encoding",
            "connection", "keep-alive",
            "x-frame-options", "content-security-policy", "content-security-policy-report-only",
            "x-content-security-policy", "x-webkit-csp",
            "cross-origin-embedder-policy", "cross-origin-opener-policy",
            "cross-origin-resource-policy", "x-content-type-options",
            "strict-transport-security"
        )
        private val STATIC_CACHE_STRIPPED_HEADERS = setOf("cache-control", "pragma", "expires")
    }

    private data class CachedAsset(
        val mimeType: String,
        val body: ByteArray
    )

    @Volatile
    var routerIp: String = routerIp
        set(value) {
            if (field != value) {
                field = value
                staticCache.clear()
            }
        }

    private val staticCache = ConcurrentHashMap<String, CachedAsset>()

    private fun log(message: String) {
        android.util.Log.d("ProxyServer", message)
        onLog?.invoke(message)
    }

    override fun stop() {
        staticCache.clear()
        super.stop()
    }

    private fun findHeader(headers: Map<String, String>, name: String): String? =
        headers.entries.firstOrNull { it.key.equals(name, ignoreCase = true) }?.value

    private fun isStaticAsset(path: String): Boolean {
        val cleanPath = path.substringBefore('?').substringBefore('#').lowercase()
        return STATIC_ASSET_EXTENSIONS.any { cleanPath.endsWith(it) }
    }

    override fun serve(session: IHTTPSession): Response {
        val method = session.method.name
        val uri = session.uri
        val query = session.queryParameterString
        val targetPath = if (!query.isNullOrBlank()) "$uri?$query" else uri
        val currentRouterIp = routerIp
        val effectiveProxyHost = findHeader(session.headers, "host")
            ?.substringBefore(':')
            ?.takeIf { it.equals("localhost", ignoreCase = true) || it == "127.0.0.1" }
            ?: proxyHost

        val staticAsset = session.method == Method.GET && isStaticAsset(targetPath)
        val cacheKey = "$currentRouterIp|$effectiveProxyHost|$targetPath"
        if (staticAsset) {
            staticCache[cacheKey]?.let { cached ->
                log("⚡ [Cache Hit] $uri (${cached.body.size}B)")
                val resp = newFixedLengthResponse(
                    Response.Status.OK,
                    cached.mimeType,
                    ByteArrayInputStream(cached.body),
                    cached.body.size.toLong()
                )
                resp.addHeader("Cache-Control", "public, max-age=3600")
                return resp
            }
        }

        log("📥 [$method] $uri")

        try {
            val bodyBytes = extractRequestBody(session)

            val browserCookie = findHeader(session.headers, "cookie")
            val mergedCookies = cookieJar.getMergedCookieHeader(browserCookie)

            val request = RouterRequest(
                method = method,
                path = targetPath,
                headers = session.headers,
                body = bodyBytes
            )

            log("📡 [Forward] -> $currentRouterIp$targetPath (${bodyBytes?.size ?: 0} body bytes)")
            val routerResponse = httpClient.sendRequest(currentRouterIp, request, mergedCookies)

            // Save Set-Cookie headers (case-insensitive across multiple header keys)
            val setCookies = routerResponse.headers.entries
                .filter { it.key.equals("Set-Cookie", ignoreCase = true) }
                .flatMap { it.value }
            if (setCookies.isNotEmpty()) {
                cookieJar.saveCookiesFromHeaders(setCookies)
                log("🍪 Saved session cookies (${setCookies.size})")

                try {
                    val cookieManager = android.webkit.CookieManager.getInstance()
                    for (cookie in setCookies) {
                        val cleaned = ResponseTransformer.cleanSetCookieHeader(cookie)
                        cookieManager.setCookie("http://$proxyHost:$proxyPort", cleaned)
                        cookieManager.setCookie("http://127.0.0.1:$proxyPort", cleaned)
                        cookieManager.setCookie("http://localhost:$proxyPort", cleaned)
                        cookieManager.setCookie("http://127.0.0.1", cleaned)
                    }
                    cookieManager.flush()
                } catch (e: Exception) {
                    log("⚠️ CookieManager sync: ${e.message}")
                }
            }

            val status = Response.Status.lookup(routerResponse.statusCode)
                ?: object : Response.IStatus {
                    override fun getRequestStatus(): Int = routerResponse.statusCode
                    override fun getDescription(): String = "${routerResponse.statusCode} ${routerResponse.statusText}"
                }

            // Determine accurate MIME type and whether content is transformable text
            val computedMimeType = getAccurateMimeType(targetPath, routerResponse.headers)
            val isTextContent = isTransformableText(targetPath, computedMimeType)
            val isHtmlDoc = computedMimeType.contains("html", ignoreCase = true)

            val finalBytes: ByteArray = if (isTextContent && routerResponse.body.isNotEmpty()) {
                val charset = try {
                    val match = Regex("""charset=([a-zA-Z0-9_-]+)""", RegexOption.IGNORE_CASE).find(computedMimeType)
                    if (match != null) java.nio.charset.Charset.forName(match.groupValues[1]) else Charsets.UTF_8
                } catch (_: Exception) {
                    Charsets.UTF_8
                }
                val bodyStr = String(routerResponse.body, charset)
                val modifiedBody = ResponseTransformer.rewriteResponseBody(
                    bodyStr,
                    currentRouterIp,
                    proxyPort,
                    effectiveProxyHost
                )
                modifiedBody.toByteArray(charset)
            } else {
                routerResponse.body
            }

            if (staticAsset &&
                routerResponse.statusCode == 200 &&
                setCookies.isEmpty() &&
                finalBytes.isNotEmpty() &&
                finalBytes.size <= MAX_CACHED_ASSET_BYTES &&
                staticCache.size < MAX_CACHED_ASSETS
            ) {
                staticCache[cacheKey] = CachedAsset(computedMimeType, finalBytes)
            }

            val cleanedSetCookies = setCookies.map { ResponseTransformer.cleanSetCookieHeader(it) }
            val response: Response = if (cleanedSetCookies.size > 1) {
                MultiSetCookieResponse(
                    status,
                    computedMimeType,
                    ByteArrayInputStream(finalBytes),
                    finalBytes.size.toLong(),
                    cleanedSetCookies
                )
            } else {
                newFixedLengthResponse(
                    status,
                    computedMimeType,
                    ByteArrayInputStream(finalBytes),
                    finalBytes.size.toLong()
                ).apply {
                    cleanedSetCookies.firstOrNull()?.let { addHeader("Set-Cookie", it) }
                }
            }

            // Forward response headers, stripping security headers that block legacy frames/iframes/MIME
            for ((headerName, headerValues) in routerResponse.headers) {
                val lower = headerName.lowercase()
                if (lower == "location") {
                    val loc = headerValues.firstOrNull() ?: ""
                    val rewrittenLoc = ResponseTransformer.rewriteRedirectLocation(
                        loc,
                        currentRouterIp,
                        proxyPort,
                        effectiveProxyHost
                    )
                    response.addHeader("Location", rewrittenLoc)
                    log("🔄 Redirect: $loc -> $rewrittenLoc")
                } else if (lower !in STRIPPED_HEADERS && (!staticAsset || lower !in STATIC_CACHE_STRIPPED_HEADERS)) {
                    response.addHeader(headerName, headerValues.joinToString(", "))
                }
            }

            if (staticAsset && routerResponse.statusCode == 200) {
                response.addHeader("Cache-Control", "public, max-age=3600")
            }

            // Log concise HTML structure for debugging
            if (isHtmlDoc && isTextContent && routerResponse.body.isNotEmpty()) {
                val charset = try {
                    val match = Regex("""charset=([a-zA-Z0-9_-]+)""", RegexOption.IGNORE_CASE).find(computedMimeType)
                    if (match != null) java.nio.charset.Charset.forName(match.groupValues[1]) else Charsets.UTF_8
                } catch (_: Exception) { Charsets.UTF_8 }
                val html = String(routerResponse.body, charset)
                val hasFrameset = html.contains("<frameset", ignoreCase = true)
                val hasBody = html.contains("<body", ignoreCase = true)
                val scriptCount = Regex("<script", RegexOption.IGNORE_CASE).findAll(html).count()
                val snippet = html.replace(Regex("\\s+"), " ").take(300)
                log("📄 [HTML] $targetPath | ${routerResponse.body.size}B | frameset=$hasFrameset body=$hasBody scripts=$scriptCount | $snippet")
            }

            log("📤 [Response] ${routerResponse.statusCode} ($computedMimeType)")
            return response

        } catch (e: Exception) {
            log("❌ Router Error: ${e.message}")
            val errorHtml = """
                <!DOCTYPE html>
                <html>
                <head><meta name="viewport" content="width=device-width, initial-scale=1.0"></head>
                <body style="font-family: sans-serif; padding: 20px; background: #0f172a; color: #f8fafc;">
                    <div style="background: #1e293b; border: 1px solid #ef4444; border-radius: 12px; padding: 20px; max-width: 500px; margin: 20px auto;">
                        <h2 style="color: #ef4444; margin-top: 0;">Connection Failed</h2>
                        <p>Could not reach router at <b>$currentRouterIp</b></p>
                        <p style="font-size: 13px; color: #94a3b8; font-family: monospace;">${e.message}</p>
                        <button onclick="location.reload()" style="background: #2563eb; color: white; border: none; padding: 10px 20px; border-radius: 6px; font-weight: bold; cursor: pointer;">Retry</button>
                    </div>
                </body>
                </html>
            """.trimIndent()

            return newFixedLengthResponse(
                Response.Status.INTERNAL_ERROR,
                "text/html; charset=utf-8",
                errorHtml
            )
        }
    }

    private class MultiSetCookieResponse(
        status: IStatus,
        mimeType: String,
        data: InputStream,
        totalBytes: Long,
        private val setCookies: List<String>
    ) : Response(status, mimeType, data, totalBytes) {
        override fun sendContentLengthHeaderIfNotAlreadyPresent(pw: PrintWriter, size: Long): Long {
            for (cookie in setCookies) {
                printHeader(pw, "Set-Cookie", cookie)
            }
            return super.sendContentLengthHeaderIfNotAlreadyPresent(pw, size)
        }
    }

    private fun extractRequestBody(session: IHTTPSession): ByteArray? {
        if (session.method != Method.POST && session.method != Method.PUT) {
            return null
        }

        val contentLength = findHeader(session.headers, "content-length")
            ?.trim()
            ?.toIntOrNull()

        if (contentLength != null) {
            if (contentLength <= 0) {
                return ByteArray(0)
            }
            val buffer = ByteArray(contentLength)
            var totalRead = 0
            while (totalRead < contentLength) {
                val read = session.inputStream.read(buffer, totalRead, contentLength - totalRead)
                if (read == -1) break
                totalRead += read
            }
            val result = if (totalRead == contentLength) buffer else buffer.copyOf(totalRead)
            log("📦 Read ${result.size}/$contentLength bytes of body from stream")
            return result
        }

        // Fallback only when Content-Length header is absent (e.g., chunked body)
        val files = HashMap<String, String>()
        return try {
            session.parseBody(files)
            val postData = files["postData"]
            if (postData != null) {
                val bytes = postData.toByteArray(Charsets.UTF_8)
                log("📦 Extracted ${bytes.size} bytes from files['postData']")
                bytes
            } else if (session.parameters.isNotEmpty()) {
                val encoded = session.parameters.entries.joinToString("&") { (k, vList) ->
                    vList.joinToString("&") { v ->
                        "${java.net.URLEncoder.encode(k, "UTF-8")}=${java.net.URLEncoder.encode(v, "UTF-8")}"
                    }
                }
                val bytes = encoded.toByteArray(Charsets.UTF_8)
                log("📦 Reconstructed ${bytes.size} bytes from form parameters")
                bytes
            } else {
                ByteArray(0)
            }
        } catch (e: Exception) {
            log("⚠️ Body parse fallback: ${e.message}")
            ByteArray(0)
        }
    }

    private fun getAccurateMimeType(path: String, headers: Map<String, List<String>>): String {
        val cleanPath = path.substringBefore('?').substringBefore('#').lowercase()
        val serverContentType = headers.entries.firstOrNull { it.key.equals("Content-Type", ignoreCase = true) }?.value?.firstOrNull()

        return when {
            cleanPath.endsWith(".css") -> "text/css; charset=utf-8"
            cleanPath.endsWith(".js") || cleanPath.endsWith(".res") -> "application/javascript; charset=utf-8"
            cleanPath.endsWith(".html") || cleanPath.endsWith(".htm") || cleanPath.endsWith(".asp") || cleanPath.endsWith(".cgi") -> "text/html; charset=utf-8"
            cleanPath.endsWith(".png") -> "image/png"
            cleanPath.endsWith(".gif") -> "image/gif"
            cleanPath.endsWith(".jpg") || cleanPath.endsWith(".jpeg") -> "image/jpeg"
            cleanPath.endsWith(".ico") -> "image/x-icon"
            cleanPath.endsWith(".svg") -> "image/svg+xml"
            cleanPath.endsWith(".json") -> "application/json; charset=utf-8"
            cleanPath.endsWith(".xml") -> "text/xml; charset=utf-8"
            !serverContentType.isNullOrBlank() -> serverContentType
            else -> "text/html; charset=utf-8"
        }
    }

    private fun isTransformableText(path: String, computedContentType: String): Boolean {
        val cleanPath = path.substringBefore('?').substringBefore('#').lowercase()
        if (cleanPath.endsWith(".html") || cleanPath.endsWith(".htm") || cleanPath.endsWith(".asp") ||
            cleanPath.endsWith(".cgi") || cleanPath.endsWith(".js") || cleanPath.endsWith(".res") ||
            cleanPath.endsWith(".css") || cleanPath.endsWith(".json") || cleanPath.endsWith(".xml") ||
            cleanPath.endsWith(".txt")
        ) {
            return true
        }
        val lower = computedContentType.lowercase()
        return lower.contains("html") || lower.contains("javascript") || lower.contains("json") ||
                lower.contains("text") || lower.contains("xml")
    }
}
