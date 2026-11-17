package com.alaa.legacyrouteropener.domain.server

import com.alaa.legacyrouteropener.data.model.RouterRequest
import com.alaa.legacyrouteropener.data.network.RouterHttpClient
import com.alaa.legacyrouteropener.data.repository.SessionCookieJar
import fi.iki.elonen.NanoHTTPD
import java.io.ByteArrayInputStream
import java.util.HashMap

class ProxyServer(
    val proxyHost: String = "127.0.0.1",
    val proxyPort: Int = 8080,
    var routerIp: String = "192.168.1.1",
    private val httpClient: RouterHttpClient,
    private val cookieJar: SessionCookieJar = SessionCookieJar(),
    private val onLog: ((String) -> Unit)? = null
) : NanoHTTPD(proxyHost, proxyPort) {

    private fun log(message: String) {
        android.util.Log.d("ProxyServer", message)
        onLog?.invoke(message)
    }

    override fun serve(session: IHTTPSession): Response {
        val method = session.method.name
        val uri = session.uri
        val query = session.queryParameterString
        val targetPath = if (!query.isNullOrBlank()) "$uri?$query" else uri

        log("📥 [$method] $uri")

        try {
            var bodyBytes: ByteArray? = null
            if (session.method == Method.POST || session.method == Method.PUT) {
                val contentLength = session.headers["content-length"]?.toIntOrNull() ?: 0
                if (contentLength > 0) {
                    val buffer = ByteArray(contentLength)
                    var totalRead = 0
                    while (totalRead < contentLength) {
                        val read = session.inputStream.read(buffer, totalRead, contentLength - totalRead)
                        if (read == -1) break
                        totalRead += read
                    }
                    bodyBytes = if (totalRead == contentLength) buffer else buffer.copyOf(totalRead)
                    log("📦 Read $totalRead bytes of body from stream")
                }

                // Fallback if inputStream was empty or already consumed
                if (bodyBytes == null || bodyBytes.isEmpty()) {
                    val files = HashMap<String, String>()
                    try {
                        session.parseBody(files)
                        val postData = files["postData"]
                        if (postData != null) {
                            bodyBytes = postData.toByteArray(Charsets.UTF_8)
                            log("📦 Extracted ${bodyBytes.size} bytes from files['postData']")
                        } else if (session.parameters.isNotEmpty()) {
                            val encoded = session.parameters.entries.joinToString("&") { (k, vList) ->
                                vList.joinToString("&") { v ->
                                    "${java.net.URLEncoder.encode(k, "UTF-8")}=${java.net.URLEncoder.encode(v, "UTF-8")}"
                                }
                            }
                            bodyBytes = encoded.toByteArray(Charsets.UTF_8)
                            log("📦 Reconstructed ${bodyBytes.size} bytes from form parameters")
                        }
                    } catch (e: Exception) {
                        log("⚠️ Body parse fallback: ${e.message}")
                    }
                }
            }

            val browserCookie = session.headers["cookie"]
            val mergedCookies = cookieJar.getMergedCookieHeader(browserCookie)

            val request = RouterRequest(
                method = method,
                path = targetPath,
                headers = session.headers,
                body = bodyBytes
            )

            log("📡 [Forward] -> https://$routerIp:443$targetPath (${bodyBytes?.size ?: 0} body bytes)")
            val routerResponse = httpClient.sendRequest(routerIp, request, mergedCookies)

            // Save Set-Cookie headers
            val setCookies = routerResponse.headers.entries.firstOrNull { it.key.equals("Set-Cookie", ignoreCase = true) }?.value ?: emptyList()
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

            val response: Response = if (isTextContent && routerResponse.body.isNotEmpty()) {
                val charset = try {
                    val match = Regex("""charset=([a-zA-Z0-9_-]+)""", RegexOption.IGNORE_CASE).find(computedMimeType)
                    if (match != null) java.nio.charset.Charset.forName(match.groupValues[1]) else Charsets.UTF_8
                } catch (_: Exception) {
                    Charsets.UTF_8
                }
                val bodyStr = String(routerResponse.body, charset)
                val modifiedBody = ResponseTransformer.rewriteResponseBody(bodyStr, routerIp, proxyPort)
                val modifiedBytes = modifiedBody.toByteArray(charset)

                newFixedLengthResponse(
                    status,
                    computedMimeType,
                    ByteArrayInputStream(modifiedBytes),
                    modifiedBytes.size.toLong()
                )
            } else {
                newFixedLengthResponse(
                    status,
                    computedMimeType,
                    ByteArrayInputStream(routerResponse.body),
                    routerResponse.body.size.toLong()
                )
            }

            // Forward response headers, stripping security headers that block legacy frames/iframes/MIME
            val strippedHeaders = setOf(
                "content-length", "content-encoding", "set-cookie", "transfer-encoding",
                "x-frame-options", "content-security-policy", "x-content-security-policy",
                "x-webkit-csp", "cross-origin-embedder-policy", "cross-origin-opener-policy",
                "cross-origin-resource-policy", "x-content-type-options"
            )
            for ((headerName, headerValues) in routerResponse.headers) {
                val lower = headerName.lowercase()
                if (lower == "location") {
                    val loc = headerValues.firstOrNull() ?: ""
                    val rewrittenLoc = ResponseTransformer.rewriteRedirectLocation(loc, routerIp, proxyPort)
                    response.addHeader("Location", rewrittenLoc)
                    log("🔄 Redirect: $loc -> $rewrittenLoc")
                } else if (lower !in strippedHeaders) {
                    response.addHeader(headerName, headerValues.joinToString(", "))
                }
            }

            for (cookie in setCookies) {
                response.addHeader("Set-Cookie", ResponseTransformer.cleanSetCookieHeader(cookie))
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
                        <p>Could not reach router at <b>https://$routerIp</b></p>
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
