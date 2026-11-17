package com.alaa.legacyrouteropener.data.repository

import java.util.concurrent.ConcurrentHashMap

class SessionCookieJar {

    companion object {
        val HUAWEI_INITIAL_COOKIES = mapOf(
            "FirstMenu" to "Admin_0",
            "SecondMenu" to "Admin_0_0",
            "ThirdMenu" to "Admin_0_0_0",
            "Language" to "en"
        )
        private val MAX_AGE_ZERO_REGEX = Regex(""";\s*max-age\s*=\s*0\b""", RegexOption.IGNORE_CASE)
    }

    private val cookies = ConcurrentHashMap<String, String>().apply {
        putAll(HUAWEI_INITIAL_COOKIES)
    }

    fun saveCookiesFromHeaders(setCookieHeaders: List<String>?) {
        if (setCookieHeaders.isNullOrEmpty()) return

        for (header in setCookieHeaders) {
            val mainPart = header.split(";").firstOrNull()?.trim() ?: continue
            val eqIndex = mainPart.indexOf('=')
            if (eqIndex > 0) {
                val key = mainPart.substring(0, eqIndex).trim()
                val value = mainPart.substring(eqIndex + 1).trim()
                if (key.isEmpty()) continue

                val isExpired = MAX_AGE_ZERO_REGEX.containsMatchIn(header)
                if (isExpired || value.isEmpty() || value.equals("deleted", ignoreCase = true)) {
                    cookies.remove(key)
                } else {
                    cookies[key] = value
                }
            }
        }
    }

    fun getMergedCookieHeader(incomingBrowserCookies: String?): String {
        val merged = LinkedHashMap<String, String>()
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
        cookies.putAll(HUAWEI_INITIAL_COOKIES)
    }
}
