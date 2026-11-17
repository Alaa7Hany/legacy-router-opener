package com.alaa.legacyrouteropener.data.repository

import java.util.concurrent.ConcurrentHashMap

class SessionCookieJar {

    private val cookies = ConcurrentHashMap<String, String>()

    fun saveCookiesFromHeaders(setCookieHeaders: List<String>?) {
        if (setCookieHeaders.isNullOrEmpty()) return

        for (header in setCookieHeaders) {
            val mainPart = header.split(";").firstOrNull()?.trim() ?: continue
            val eqIndex = mainPart.indexOf('=')
            if (eqIndex > 0) {
                val key = mainPart.substring(0, eqIndex).trim()
                val value = mainPart.substring(eqIndex + 1).trim()
                if (key.isNotEmpty() && value.isNotEmpty()) {
                    cookies[key] = value
                }
            }
        }
    }

    fun getMergedCookieHeader(incomingBrowserCookies: String?): String {
        val merged = mutableMapOf<String, String>()
        merged.putAll(cookies)

        if (!incomingBrowserCookies.isNullOrBlank()) {
            incomingBrowserCookies.split(";").forEach { part ->
                val eqIndex = part.indexOf('=')
                if (eqIndex > 0) {
                    val key = part.substring(0, eqIndex).trim()
                    val value = part.substring(eqIndex + 1).trim()
                    if (key.isNotEmpty()) {
                        merged[key] = value
                    }
                }
            }
        }

        return merged.entries.joinToString("; ") { "${it.key}=${it.value}" }
    }

    fun clear() {
        cookies.clear()
    }
}
