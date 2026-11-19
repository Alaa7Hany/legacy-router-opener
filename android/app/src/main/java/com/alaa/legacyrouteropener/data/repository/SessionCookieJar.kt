package com.alaa.legacyrouteropener.data.repository

internal class SessionCookieJar(private val seed: Map<String, String> = emptyMap()) {
    private val stored = LinkedHashMap<String, String>()

    @Synchronized
    fun save(cookies: List<String>) {
        for (header in cookies) {
            val main = header.substringBefore(';').trim()
            val eq = main.indexOf('=')
            if (eq <= 0) continue
            val name = main.substring(0, eq).trim()
            val value = main.substring(eq + 1).trim()
            if (name.isEmpty()) continue
            val expired = maxAgeExpired.containsMatchIn(header)
            if (expired || value.isEmpty() || value.equals("deleted", ignoreCase = true)) {
                stored.remove(name)
            } else {
                stored[name] = value
            }
        }
    }

    @Synchronized
    fun merge(browserCookies: List<String>): String {
        val merged = LinkedHashMap<String, String>()
        merged.putAll(seed)
        merged.putAll(stored)
        for (header in browserCookies) {
            for (part in header.split(';')) {
                val eq = part.indexOf('=')
                if (eq <= 0) continue
                val name = part.substring(0, eq).trim()
                val value = part.substring(eq + 1).trim()
                if (name.isNotEmpty()) merged[name] = value
            }
        }
        return merged.entries.joinToString("; ") { "${it.key}=${it.value}" }
    }

    private companion object {
        val maxAgeExpired = Regex(""";\s*max-age\s*=\s*(?:0|-\d+)\b""", RegexOption.IGNORE_CASE)
    }
}
