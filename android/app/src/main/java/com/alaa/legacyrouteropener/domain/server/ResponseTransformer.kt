package com.alaa.legacyrouteropener.domain.server

object ResponseTransformer {

    fun rewriteResponseBody(
        body: String,
        routerIp: String,
        proxyPort: Int
    ): String {
        val proxyHost = "127.0.0.1:$proxyPort"

        // 1. Standard URLs (http://192.168.1.1:443 or https://192.168.1.1)
        val urlRegex = Regex("""https?://${Regex.escape(routerIp)}(:\d+)?""", RegexOption.IGNORE_CASE)
        var modified = urlRegex.replace(body, "http://$proxyHost")

        // 2. Escaped URLs in JSON or inline JS (https:\/\/192.168.1.1)
        val escapedUrlRegex = Regex("""https?:\\/\\/${Regex.escape(routerIp)}(:\d+)?""", RegexOption.IGNORE_CASE)
        modified = escapedUrlRegex.replace(modified, """http:\/\/$proxyHost""")

        // 3. Protocol-relative URLs (//192.168.1.1:443 or //192.168.1.1)
        val protocolRelativeRegex = Regex("""(?<=['"\(])//${Regex.escape(routerIp)}(:\d+)?""", RegexOption.IGNORE_CASE)
        modified = protocolRelativeRegex.replace(modified, "//$proxyHost")

        return modified
    }

    fun cleanSetCookieHeader(cookieHeader: String): String {
        var cleaned = cookieHeader
            .replace(Regex(""";\s*domain=[^;]+""", RegexOption.IGNORE_CASE), "")
            .replace(Regex(""";\s*secure""", RegexOption.IGNORE_CASE), "")
            .replace(Regex(""";\s*SameSite=None""", RegexOption.IGNORE_CASE), "; SameSite=Lax")
            .trim()
        if (!cleaned.contains("path=", ignoreCase = true)) {
            cleaned = "$cleaned; Path=/"
        }
        return cleaned
    }

    fun rewriteRedirectLocation(location: String, routerIp: String, proxyPort: Int): String {
        val proxyHost = "http://127.0.0.1:$proxyPort"
        val urlRegex = Regex("""https?://${Regex.escape(routerIp)}(:\d+)?""", RegexOption.IGNORE_CASE)
        return urlRegex.replace(location, proxyHost)
    }
}
