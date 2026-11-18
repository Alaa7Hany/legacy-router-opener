package com.alaa.legacyrouteropener.data.repository

import java.net.CookieManager
import java.net.CookiePolicy
import java.net.URI

internal class SessionCookieJar(seedHuawei: Boolean) {
    private val manager = CookieManager(null, CookiePolicy.ACCEPT_ORIGINAL_SERVER)
    private val seed = if (seedHuawei) mapOf(
        "FirstMenu" to "Admin_0",
        "SecondMenu" to "Admin_0_0",
        "ThirdMenu" to "Admin_0_0_0",
        "Language" to "en"
    ) else emptyMap()

    @Synchronized
    fun save(uri: URI, cookies: List<String>) {
        manager.put(uri, mapOf("Set-Cookie" to cookies))
    }

    @Synchronized
    fun merge(uri: URI, browserCookies: List<String>): String {
        val stored = manager.get(uri, emptyMap())["Cookie"].orEmpty()
            .map { it.substringBefore("; $") }
        val keys = (browserCookies + stored).flatMap { it.split(';') }
            .map { it.substringBefore('=').trim() }.toSet()
        val seeded = seed.filterKeys { it !in keys }.map { (key, value) -> "$key=$value" }
        val storedOnly = stored.flatMap { it.split(';') }.map(String::trim)
            .filter { it.isNotEmpty() && !it.startsWith('$') }
            .filter { cookie ->
                browserCookies.none { header ->
                    header.split(';').any { it.trim().substringBefore('=') == cookie.substringBefore('=') }
                }
            }
        return (browserCookies + storedOnly + seeded).joinToString("; ")
    }
}
