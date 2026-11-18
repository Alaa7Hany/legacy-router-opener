package com.alaa.legacyrouteropener.data.model

import java.net.Inet6Address
import java.net.InetAddress
import java.util.Locale

data class Endpoint(
    val host: String,
    val port: Int,
    val tls: Boolean,
    val explicitScheme: Boolean,
    val explicitPort: Boolean
) {
    val bracketedHost get() = if (':' in host) "[$host]" else host
    val authority get() = if (port == if (tls) 443 else 80) bracketedHost else "$bracketedHost:$port"
    val url get() = "${if (tls) "https" else "http"}://$authority"

    companion object {
        private val label = Regex("[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?")
        private val digits = Regex("[0-9]+")

        fun parse(input: String): Endpoint {
            val value = input.trim(' ')
            require(value.isNotEmpty()) { "address_required" }
            require(value.none(Char::isWhitespace)) { "address_spaces" }
            require(value.none { it.code < 32 || it.code == 127 || it in "@<>\"'\\?#%" }) { "address_invalid" }
            val scheme = value.substringBefore("://", "").lowercase(Locale.ROOT)
            require("://" !in value || scheme in setOf("http", "https")) { "address_invalid" }
            val authority = value.substringAfter("://").substringBefore('/')
            val host: String
            val portText: String?
            when {
                authority.startsWith('[') -> {
                    val close = authority.indexOf(']')
                    require(close > 1) { "address_invalid" }
                    host = authority.substring(1, close)
                    val suffix = authority.substring(close + 1)
                    require(suffix.isEmpty() || suffix.startsWith(':')) { "address_invalid" }
                    portText = suffix.takeIf { it.isNotEmpty() }?.drop(1)
                    require(':' in host) { "address_invalid" }
                }
                authority.count { it == ':' } == 1 -> {
                    host = authority.substringBefore(':')
                    portText = authority.substringAfter(':')
                }
                else -> {
                    host = authority
                    portText = null
                }
            }
            val normalized = host.lowercase(Locale.ROOT)
            val validHost = when {
                ':' in normalized -> normalized.matches(Regex("[a-f0-9:.]+")) &&
                    runCatching { InetAddress.getByName(normalized) is Inet6Address }.getOrDefault(false)
                normalized.all { it in '0'..'9' || it == '.' } -> normalized.split('.').let { parts ->
                    parts.size == 4 && parts.all {
                        (it == "0" || !it.startsWith('0')) && (it.toIntOrNull() ?: -1) in 0..255
                    }
                }
                else -> normalized.length <= 253 && normalized.split('.').all(label::matches)
            }
            require(validHost) { "address_invalid" }
            val port = portText?.let {
                require(digits.matches(it)) { "address_invalid" }
                it.toIntOrNull()?.takeIf { port -> port in 1..65535 }
                    ?: throw IllegalArgumentException("address_invalid")
            } ?: if (scheme == "http") 80 else 443
            val tls = scheme == "https" || (scheme.isEmpty() && port != 80)
            require(normalized != "localhost" && !normalized.endsWith(".localhost")) { "address_self_blocked" }
            return Endpoint(normalized, port, tls, scheme.isNotEmpty(), portText != null)
        }
    }
}

fun redactQuery(value: String): String = value.substringBefore('?') + if ('?' in value) "?<redacted>" else ""
