package com.alaa.legacyrouteropener.data.network

import java.io.EOFException
import java.io.IOException
import java.io.InputStream
import java.io.OutputStream
import java.util.Locale
import java.util.zip.GZIPInputStream
import java.util.zip.Inflater
import java.util.zip.InflaterInputStream

internal typealias Headers = Map<String, List<String>>

internal fun Headers.first(name: String) = get(name)?.firstOrNull()

internal class HttpError(val status: Int) : IOException("HTTP $status")

internal object HttpWireCodec {
    private const val LINE_LIMIT = 8192
    private const val HEADER_LIMIT = 65536
    private const val FIELD_LIMIT = 100
    private val token = Regex("[!#$%&'*+.^_\u0060|~0-9A-Za-z-]+")
    private val originPath = Regex("/[\\x21-\\x7e]*")
    private val lengthValue = Regex("[0-9]+")
    private val hexSize = Regex("[0-9a-fA-F]+")

    data class RequestHead(val method: String, val path: String, val headers: Headers)
    data class ResponseHead(val status: Int, val headers: Headers)

    fun readRequest(input: InputStream): RequestHead {
        val start = line(input, strict = true).split(' ')
        if (start.size != 3 || !token.matches(start[0]) || !originPath.matches(start[1]) ||
            start[1].startsWith("//") || '#' in start[1] ||
            start[2] !in setOf("HTTP/1.1", "HTTP/1.0")
        ) throw HttpError(400)
        val headers = readHeaders(input, strict = true)
        validateFraming(headers)
        if (start[2] == "HTTP/1.0" && headers.containsKey("transfer-encoding")) throw HttpError(400)
        return RequestHead(start[0], start[1], headers)
    }

    fun readResponse(input: InputStream): ResponseHead {
        repeat(8) {
            val start = line(input, strict = false).split(' ', limit = 3)
            val status = start.getOrNull(1)?.toIntOrNull()
            if (start.firstOrNull() !in setOf("HTTP/1.0", "HTTP/1.1") || status == null ||
                status !in 100..599 || start[1].length != 3 || status == 101
            ) throw IOException("Invalid response status")
            val headers = readHeaders(input, strict = false)
            validateFraming(headers)
            if (status >= 200) return ResponseHead(status, headers)
        }
        throw IOException("Too many informational responses")
    }

    private fun readHeaders(input: InputStream, strict: Boolean): Headers {
        val headers = linkedMapOf<String, MutableList<String>>()
        var bytes = 0
        var count = 0
        while (true) {
            val line = line(input, strict)
            bytes += line.length + 2
            if (line.isEmpty()) return headers
            if (bytes > HEADER_LIMIT || ++count > FIELD_LIMIT) throw HttpError(431)
            val colon = line.indexOf(':')
            if (colon <= 0) throw HttpError(400)
            val name = line.substring(0, colon)
            val value = line.substring(colon + 1).trim(' ', '\t')
            if (!token.matches(name) || value.any { it.code < 32 && it != '\t' || it.code == 127 }) {
                throw HttpError(400)
            }
            headers.getOrPut(name.lowercase(Locale.ROOT)) { mutableListOf() }.add(value)
        }
    }

    private fun line(input: InputStream, strict: Boolean): String {
        val line = StringBuilder()
        while (true) {
            when (val next = input.read()) {
                -1 -> throw EOFException("Incomplete HTTP line")
                10 -> if (!strict) return line.toString() else throw HttpError(400)
                13 -> {
                    if (input.read() != 10) throw HttpError(400)
                    return line.toString()
                }
                else -> {
                    if (line.length == LINE_LIMIT) throw HttpError(431)
                    if (next < 32 && next != 9 || next == 127) throw HttpError(400)
                    line.append(next.toChar())
                }
            }
        }
    }

    private fun validateFraming(headers: Headers) {
        val transfer = headers["transfer-encoding"]
        val lengths = headers["content-length"]
        if (transfer != null && (lengths != null || transfer.size != 1 ||
                !transfer.single().equals("chunked", ignoreCase = true))) throw HttpError(400)
        if (lengths != null && (lengths.size != 1 || !lengthValue.matches(lengths.single()) ||
                lengths.single().toLongOrNull() == null)) throw HttpError(400)
    }

    fun body(input: InputStream, headers: Headers, budget: MemoryBudget, closeDelimited: Boolean): WireBody {
        val result = WireBody(budget)
        try {
            val buffer = ByteArray(8192)
            when {
                headers.containsKey("transfer-encoding") -> {
                    while (true) {
                        val sizeText = line(input, strict = true).substringBefore(';').trim()
                        if (!hexSize.matches(sizeText)) throw HttpError(400)
                        val size = sizeText.toLongOrNull(16) ?: throw BodyTooLarge()
                        if (size == 0L) {
                            readHeaders(input, strict = true)
                            break
                        }
                        copyExact(input, result, buffer, size)
                        if (input.read() != 13 || input.read() != 10) throw HttpError(400)
                    }
                }
                headers.first("content-length") != null ->
                    copyExact(input, result, buffer, headers.first("content-length")!!.toLong())
                closeDelimited -> copyToEnd(input, result, buffer)
            }
            return result
        } catch (failure: Throwable) {
            result.close()
            throw failure
        }
    }

    private fun copyExact(input: InputStream, output: WireBody, buffer: ByteArray, size: Long) {
        if (size > 32L * MIB - output.size) throw BodyTooLarge()
        var left = size
        while (left > 0) {
            val count = input.read(buffer, 0, minOf(buffer.size.toLong(), left).toInt())
            if (count < 0) throw EOFException("Incomplete HTTP body")
            output.write(buffer, 0, count)
            left -= count
        }
    }

    private fun copyToEnd(input: InputStream, output: WireBody, buffer: ByteArray) {
        while (true) {
            val count = input.read(buffer)
            if (count < 0) return
            output.write(buffer, 0, count)
        }
    }

    fun decode(raw: WireBody, encoding: String, budget: MemoryBudget): WireBody {
        val gzip = encoding.equals("gzip", ignoreCase = true) || encoding.equals("x-gzip", ignoreCase = true)
        if (!gzip && !encoding.equals("deflate", ignoreCase = true)) throw IOException("Unsupported content encoding")
        fun decompress(nowrap: Boolean): WireBody {
            val result = WireBody(budget)
            val inflater = if (gzip) null else Inflater(nowrap)
            try {
                val stream = if (inflater == null) GZIPInputStream(raw.inputStream()) else
                    InflaterInputStream(raw.inputStream(), inflater)
                stream.use { copyToEnd(it, result, ByteArray(8192)) }
                return result
            } catch (failure: Throwable) {
                result.close()
                throw failure
            } finally {
                inflater?.end()
            }
        }
        return if (gzip) decompress(false) else try {
            decompress(false)
        } catch (_: java.util.zip.ZipException) {
            decompress(true)
        }
    }

    fun writeHead(output: OutputStream, start: String, headers: Headers, canonicalNames: Boolean = false) {
        output.write("$start\r\n".toByteArray(Charsets.ISO_8859_1))
        headers.forEach { (name, values) ->
            val wireName = if (canonicalNames) canonicalHeaderName(name) else name
            values.forEach { value -> output.write("$wireName: $value\r\n".toByteArray(Charsets.ISO_8859_1)) }
        }
        output.write(byteArrayOf(13, 10))
    }

    private fun canonicalHeaderName(name: String): String = name.split('-').joinToString("-") { segment ->
        segment.replaceFirstChar { ch -> if (ch.isLowerCase()) ch.uppercase(Locale.ROOT) else ch.toString() }
    }

    fun forwardedHeaders(headers: Headers): Headers {
        val nominated = headers["connection"].orEmpty().flatMap { value ->
            value.split(',').map { it.trim().lowercase(Locale.ROOT) }
        }
        return headers.filterKeys { it !in hopHeaders && it !in nominated }
    }

    fun hasBody(method: String, status: Int) = method != "HEAD" && status !in setOf(204, 205, 304)

    private val hopHeaders = setOf(
        "connection", "keep-alive", "proxy-connection", "proxy-authenticate", "proxy-authorization",
        "te", "trailer", "transfer-encoding", "upgrade", "content-length", "expect"
    )
}
