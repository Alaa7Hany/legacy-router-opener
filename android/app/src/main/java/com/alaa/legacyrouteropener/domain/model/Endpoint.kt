package com.alaa.legacyrouteropener.domain.model

import java.net.Inet6Address
import java.net.InetAddress
import java.util.Locale

class EndpointParseException(val error: ValidationError) : IllegalArgumentException(error.messageKey)

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

        private fun requireEndpoint(condition: Boolean, error: ValidationError) {
            if (!condition) throw EndpointParseException(error)
        }

        fun parse(input: String): Endpoint {
            val value = input.trim(' ')
            requireEndpoint(value.isNotEmpty(), ValidationError.ADDRESS_REQUIRED)
            requireEndpoint(value.none(Char::isWhitespace), ValidationError.ADDRESS_SPACES)
            requireEndpoint(value.none { it.code < 32 || it.code == 127 || it in "@<>\"'\\?#%" }, ValidationError.ADDRESS_INVALID)
            val scheme = value.substringBefore("://", "").lowercase(Locale.ROOT)
            requireEndpoint("://" !in value || scheme in setOf("http", "https"), ValidationError.ADDRESS_INVALID)
            val authority = value.substringAfter("://").substringBefore('/')
            val host: String
            val portText: String?
            when {
                authority.startsWith('[') -> {
                    val close = authority.indexOf(']')
                    requireEndpoint(close > 1, ValidationError.ADDRESS_INVALID)
                    host = authority.substring(1, close)
                    val suffix = authority.substring(close + 1)
                    requireEndpoint(suffix.isEmpty() || suffix.startsWith(':'), ValidationError.ADDRESS_INVALID)
                    portText = suffix.takeIf { it.isNotEmpty() }?.drop(1)
                    requireEndpoint(':' in host, ValidationError.ADDRESS_INVALID)
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
            requireEndpoint(validHost, ValidationError.ADDRESS_INVALID)
            val port = portText?.let {
                requireEndpoint(digits.matches(it), ValidationError.ADDRESS_INVALID)
                it.toIntOrNull()?.takeIf { port -> port in 1..65535 }
                    ?: throw EndpointParseException(ValidationError.ADDRESS_INVALID)
            } ?: if (scheme == "http") 80 else 443
            val tls = scheme == "https" || (scheme.isEmpty() && port != 80)
            requireEndpoint(normalized != "localhost" && !normalized.endsWith(".localhost"), ValidationError.ADDRESS_SELF_BLOCKED)
            return Endpoint(normalized, port, tls, scheme.isNotEmpty(), portText != null)
        }
    }
}

fun redactQuery(value: String): String = value.substringBefore('?') + if ('?' in value) "?<redacted>" else ""
