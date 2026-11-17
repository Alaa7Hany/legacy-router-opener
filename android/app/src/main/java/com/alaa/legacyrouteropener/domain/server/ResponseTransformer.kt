package com.alaa.legacyrouteropener.domain.server

object ResponseTransformer {

    fun extractHost(routerEndpoint: String): String {
        val cleaned = routerEndpoint.trim()
            .removePrefix("https://")
            .removePrefix("http://")
            .substringBefore('/')
            .trim()
        return if (cleaned.startsWith("[")) {
            cleaned
        } else {
            cleaned.substringBefore(':').trim().ifEmpty { "192.168.1.1" }
        }
    }

    fun rewriteResponseBody(
        body: String,
        routerIp: String,
        proxyPort: Int,
        proxyHost: String = "127.0.0.1"
    ): String {
        val host = extractHost(routerIp)
        val escapedHost = Regex.escape(host)
        val proxyAuthority = "$proxyHost:$proxyPort"

        // 1. Standard URLs (http://192.168.1.1:443 or https://192.168.1.1)
        val urlRegex = Regex("""https?://$escapedHost(:\d+)?""", RegexOption.IGNORE_CASE)
        var modified = urlRegex.replace(body, Regex.escapeReplacement("http://$proxyAuthority"))

        // 2. Escaped URLs in JSON or inline JS (https:\/\/192.168.1.1)
        val escapedUrlRegex = Regex("""https?:\\/\\/$escapedHost(:\d+)?""", RegexOption.IGNORE_CASE)
        modified = escapedUrlRegex.replace(modified, Regex.escapeReplacement("""http:\/\/$proxyAuthority"""))

        // 3. Protocol-relative URLs (//192.168.1.1:443 or //192.168.1.1)
        val protocolRelativeRegex = Regex("""(?<=['"\(])//$escapedHost(:\d+)?""", RegexOption.IGNORE_CASE)
        modified = protocolRelativeRegex.replace(modified, Regex.escapeReplacement("//$proxyAuthority"))

        // 4. Bare quoted router IP literals ('192.168.1.1' or "192.168.1.1")
        val quotedIpRegex = Regex("""(['"])$escapedHost(:\d+)?\1""")
        modified = quotedIpRegex.replace(modified) { match ->
            val quote = match.groupValues[1]
            "$quote$proxyAuthority$quote"
        }

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

    fun rewriteRedirectLocation(
        location: String,
        routerIp: String,
        proxyPort: Int,
        proxyHost: String = "127.0.0.1"
    ): String {
        val trimmed = location.trim()
        if (trimmed.isEmpty()) return trimmed

        val host = extractHost(routerIp)
        val escapedHost = Regex.escape(host)
        val proxyBase = "http://$proxyHost:$proxyPort"

        val urlRegex = Regex("""https?://$escapedHost(:\d+)?""", RegexOption.IGNORE_CASE)
        if (urlRegex.containsMatchIn(trimmed)) {
            return urlRegex.replace(trimmed, Regex.escapeReplacement(proxyBase))
        }

        val protocolRelativeRegex = Regex("""^//$escapedHost(:\d+)?""", RegexOption.IGNORE_CASE)
        if (protocolRelativeRegex.containsMatchIn(trimmed)) {
            return protocolRelativeRegex.replace(trimmed, Regex.escapeReplacement(proxyBase))
        }

        return trimmed
    }
}
