package com.alaa.legacyrouteropener.data.proxy

import com.alaa.legacyrouteropener.domain.model.Endpoint
import com.alaa.legacyrouteropener.data.network.BodyTooLarge
import com.alaa.legacyrouteropener.data.network.MIB
import com.alaa.legacyrouteropener.data.network.MemoryBudget
import com.alaa.legacyrouteropener.data.network.WireBody
import java.io.InputStreamReader
import java.io.OutputStreamWriter
import java.nio.CharBuffer
import java.nio.charset.Charset
import java.util.Locale
import java.util.regex.Pattern

internal class ResponseTransformer(private val endpoint: Endpoint, private val authority: String) {
    private val proxyOrigin = "http://$authority"
    private val host = Pattern.quote(endpoint.bracketedHost)
    private val rewrite = Pattern.compile(
        """(https?://)$host(?::[0-9]{1,5})?$boundary|""" +
            """(https?:\\/\\/)$host(?::[0-9]{1,5})?$boundary|""" +
            """(?<=['"=(\s])(//)$host(?::[0-9]{1,5})?$boundary|""" +
            """(['"])$host(?::[0-9]{1,5})?\4""",
        Pattern.CASE_INSENSITIVE
    )
    private val locationPattern = Regex("""^(https?:)?//(\[[^\]]+]|[^/:?#]+)(?::[0-9]{1,5})?([/?#].*)?$""",
        RegexOption.IGNORE_CASE)

    fun redirect(location: String, singleLabel: Boolean): String {
        val parts = locationPattern.matchEntire(location.trim()) ?: return location
        val destination = parts.groupValues[2]
        if (!destination.equals(endpoint.bracketedHost, true) && !(singleLabel && singleLabelPattern.matches(destination))) {
            return location
        }
        val suffix = parts.groupValues[3]
        return proxyOrigin + if (suffix.isEmpty() || !suffix.startsWith('/')) "/$suffix" else suffix
    }

    fun transform(input: WireBody, mimeType: String, budget: MemoryBudget): WireBody {
        val type = mimeType.substringBefore(';').lowercase(Locale.ROOT)
        if (!type.startsWith("text/") && type !in textMimeTypes && !type.endsWith("+xml") &&
            !type.endsWith("+json")) return input
        val nul = input.inputStream().use { stream ->
            var found = false
            repeat(minOf(input.size, 1024)) { if (stream.read() == 0) found = true }
            found
        }
        // UTF-16 and UTF-32 text legitimately include NUL bytes in their wire representation.
        val charsetName = charsetParam.find(mimeType)?.groupValues?.get(1) ?: "UTF-8"
        if (nul && !charsetName.startsWith("UTF-16", true) && !charsetName.startsWith("UTF-32", true)) return input
        if (input.size > MAX_TEXT) throw BodyTooLarge()
        val charset = runCatching { Charset.forName(charsetName) }.getOrDefault(Charsets.UTF_8)
        val charCapacity = kotlin.math.ceil(input.size.toDouble() * charset.newDecoder().maxCharsPerByte()).toInt()
        budget.reserve(charCapacity * Char.SIZE_BYTES).use {
            val chars = CharArray(charCapacity)
            val size = InputStreamReader(input.inputStream(), charset).use { reader ->
                var count = 0
                while (count < chars.size) {
                    val next = reader.read(chars, count, chars.size - count)
                    if (next < 0) break
                    count += next
                }
                if (reader.read() != -1) throw BodyTooLarge()
                count
            }
            val matcher = rewrite.matcher(CharBuffer.wrap(chars, 0, size))
            if (!matcher.find()) return input
            val output = WireBody(budget, MAX_TEXT)
            try {
                // Closing the encoder flushes stateful encodings, while the body remains owned by the caller.
                OutputStreamWriter(output.asOutputStream(), charset).use { writer ->
                    var position = 0
                    do {
                        writer.write(chars, position, matcher.start() - position)
                        val replacement = when {
                            matcher.group(1) != null -> proxyOrigin
                            matcher.group(2) != null -> """http:\/\/$authority"""
                            matcher.group(3) != null -> "//$authority"
                            else -> {
                                val quote = checkNotNull(matcher.group(4))
                                quote + authority + quote
                            }
                        }
                        writer.write(replacement)
                        position = matcher.end()
                    } while (matcher.find())
                    writer.write(chars, position, size - position)
                }
                return output
            } catch (failure: Throwable) {
                output.close()
                throw failure
            }
        }
    }

    companion object {
        private const val MAX_TEXT = 8 * MIB
        private const val boundary = """(?![A-Za-z0-9_.@%:\-])"""
        private val singleLabelPattern = Regex("[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?")
        private val charsetParam = Regex("""(?:^|;)\s*charset\s*=\s*["']?([^\s;"']+)""", RegexOption.IGNORE_CASE)
        private val textMimeTypes = setOf("application/javascript", "application/json", "application/xml",
            "application/x-javascript", "image/svg+xml", "application/xhtml+xml")
        private val extensions = mapOf(
            "css" to "text/css", "js" to "application/javascript", "mjs" to "application/javascript",
            "res" to "application/javascript",
            "html" to "text/html", "htm" to "text/html", "shtml" to "text/html",
            "asp" to "text/html", "cgi" to "text/html",
            "json" to "application/json", "xml" to "application/xml", "txt" to "text/plain",
            "png" to "image/png", "jpg" to "image/jpeg", "jpeg" to "image/jpeg", "ico" to "image/x-icon",
            "gif" to "image/gif", "bmp" to "image/bmp", "webp" to "image/webp", "svg" to "image/svg+xml",
            "woff" to "font/woff", "woff2" to "font/woff2", "ttf" to "font/ttf", "otf" to "font/otf",
            "pdf" to "application/pdf", "zip" to "application/zip", "bin" to "application/octet-stream"
        )
        private val assets = extensions.keys - setOf("html", "htm", "shtml", "asp", "cgi", "json", "xml", "txt", "bin", "zip", "pdf")

        private fun extension(path: String) = path.substringBefore('?').substringAfterLast('/')
            .substringAfterLast('.', "").lowercase(Locale.ROOT)

        fun isStatic(path: String) = extension(path) in assets

        fun mimeType(path: String, upstream: String?): String {
            val inferred = extensions[extension(path)]
            if (inferred == null) return upstream ?: "application/octet-stream"
            val subtype = upstream?.substringBefore(';')?.trim()?.lowercase(Locale.ROOT)
            if (subtype == null || subtype == "application/octet-stream" || subtype == "text/plain" ||
                subtype == "text/js" || (subtype == "text/html" && isStatic(path))) {
                val charset = upstream?.let { charsetParam.find(it)?.groupValues?.get(1) }
                return if (charset == null || !inferred.startsWith("text/") && inferred !in textMimeTypes) inferred
                    else "$inferred; charset=$charset"
            }
            return upstream ?: inferred
        }

        fun cleanCookie(header: String): String {
            val parts = header.split(';').map(String::trim).filter(String::isNotEmpty)
            val main = parts.firstOrNull() ?: return ""
            val value = main.substringAfter('=', "").trim()
            val attrs = parts.drop(1)
            val isDeleting = value.isEmpty() || value.equals("deleted", true) ||
                attrs.any { it.startsWith("Max-Age=", true) && (it.substringAfter('=').trim().toLongOrNull() ?: 1L) <= 0L }
            val cleaned = mutableListOf(main)
            var hasPath = false
            for (attr in attrs) {
                val name = attr.substringBefore('=').trim()
                if (name.equals("Domain", true) || attr.equals("Secure", true) || attr.equals("Partitioned", true)) continue
                if (!isDeleting && name.equals("Expires", true)) continue
                if (name.equals("Path", true)) {
                    hasPath = true
                    cleaned.add("Path=/")
                    continue
                }
                cleaned.add(if (attr.equals("SameSite=None", true)) "SameSite=Lax" else attr)
            }
            if (!hasPath) cleaned.add("Path=/")
            return cleaned.joinToString("; ")
        }
    }
}
