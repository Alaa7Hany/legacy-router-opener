package com.alaa.legacyrouteropener.domain.server

import com.alaa.legacyrouteropener.data.model.RouterRequest
import com.alaa.legacyrouteropener.data.model.RouterResponse
import com.alaa.legacyrouteropener.data.network.*
import java.io.BufferedInputStream
import java.io.Closeable
import java.io.EOFException
import java.io.IOException
import java.net.InetAddress
import java.net.InetSocketAddress
import java.net.ServerSocket
import java.net.Socket
import java.net.SocketTimeoutException
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.RejectedExecutionException
import java.util.concurrent.ThreadPoolExecutor
import java.util.concurrent.TimeUnit

internal class LocalHttpServer(
    private val hostname: String,
    preferredPort: Int,
    private val budget: MemoryBudget,
    private val handler: (RouterRequest) -> RouterResponse,
    private val headerTimeoutMillis: Long = 5_000,
    private val bodyTimeoutMillis: Long = 15_000,
    private val writeTimeoutMillis: Long = 15_000
) : Closeable {
    private val clients = SocketRegistry()
    private val listener = ServerSocket().apply {
        try {
            reuseAddress = true
            bind(InetSocketAddress(InetAddress.getByAddress(byteArrayOf(127, 0, 0, 1)), preferredPort), 16)
        } catch (failure: IOException) {
            close()
            throw failure
        }
    }
    private val executor = ThreadPoolExecutor(
        16, 16, 30, TimeUnit.SECONDS, ArrayBlockingQueue(32),
        { work -> Thread(work, "router-client").apply { isDaemon = true } }
    ).apply { allowCoreThreadTimeOut(true) }
    val port: Int = listener.localPort
    private val authority = "$hostname:$port"

    fun start() {
        Thread(::accept, "router-listener").apply { isDaemon = true }.start()
    }

    private fun accept() {
        while (!listener.isClosed) {
            val socket = try {
                clients.track(listener.accept())
            } catch (_: IOException) {
                return
            }
            val accepted = System.nanoTime()
            try {
                executor.execute {
                    try {
                        socket.use { serve(it, accepted) }
                    } finally {
                        clients.forget(socket)
                    }
                }
            } catch (_: RejectedExecutionException) {
                clients.forget(socket)
                runCatching { socket.close() }
            }
        }
    }

    private fun serve(socket: Socket, accepted: Long) {
        try {
            val input = BufferedInputStream(socket.getInputStream())
            val elapsed = TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - accepted)
            if (elapsed >= headerTimeoutMillis) throw HttpError(408)
            val head = SocketDeadline(socket, headerTimeoutMillis - elapsed).use { deadline ->
                deadline.check()
                HttpWireCodec.readRequest(input)
            }
            val hosts = head.headers["host"]
            if (hosts?.size != 1) throw HttpError(400)
            if (!hosts.single().equals(authority, ignoreCase = true)) throw HttpError(404)
            if (head.method in setOf("CONNECT", "TRACE") || head.headers.containsKey("upgrade")) throw HttpError(405)
            val expectation = head.headers["expect"]
            if (expectation != null) {
                if (expectation.size != 1 || !expectation.single().equals("100-continue", true)) throw HttpError(417)
                if ((head.headers.first("content-length")?.toLong() ?: 0) > 32L * MIB) throw HttpError(413)
                SocketDeadline(socket, writeTimeoutMillis).use {
                    socket.getOutputStream().write("HTTP/1.1 100 Continue\r\n\r\n".toByteArray(Charsets.US_ASCII))
                }
            }
            val body = SocketDeadline(socket, bodyTimeoutMillis).use { deadline ->
                deadline.check()
                HttpWireCodec.body(input, head.headers, budget, closeDelimited = false)
            }
            body.use {
                handler(RouterRequest(head.method, head.path, head.headers, body)).use { response ->
                    writeResponse(socket, response, head.method)
                }
            }
        } catch (failure: HttpError) {
            sendError(socket, failure.status)
        } catch (_: BodyTooLarge) {
            sendError(socket, 413)
        } catch (_: BudgetExceeded) {
            sendError(socket, 503)
        } catch (_: SocketTimeoutException) {
            sendError(socket, 408)
        } catch (_: EOFException) {
            sendError(socket, 400)
        } catch (_: IOException) {
            // The peer disconnected or a deadline closed the socket.
        }
    }

    private fun writeResponse(socket: Socket, response: RouterResponse, method: String) {
        val headers = HttpWireCodec.forwardedHeaders(response.headers).toMutableMap()
        headers["connection"] = listOf("close")
        if (response.status != 204 && response.status != 304) {
            val length = if (method == "HEAD") response.headers.first("content-length") ?: "0"
                else response.body.size.toString()
            headers["content-length"] = listOf(length)
        }
        SocketDeadline(socket, writeTimeoutMillis).use { deadline ->
            deadline.check()
            val output = socket.getOutputStream()
            HttpWireCodec.writeHead(output, "HTTP/1.1 ${response.status} ${reason(response.status)}", headers)
            if (HttpWireCodec.hasBody(method, response.status)) response.body.writeTo(output)
            output.flush()
        }
    }

    private fun sendError(socket: Socket, status: Int) {
        if (socket.isClosed) return
        runCatching {
            SocketDeadline(socket, 1_000).use {
                HttpWireCodec.writeHead(socket.getOutputStream(), "HTTP/1.1 $status ${reason(status)}",
                    mapOf("connection" to listOf("close"), "content-length" to listOf("0")))
            }
        }
    }

    override fun close() {
        runCatching { listener.close() }
        clients.close()
        executor.shutdownNow()
    }

    private fun reason(status: Int): String = when (status) {
        200 -> "OK"
        204 -> "No Content"
        302 -> "Found"
        400 -> "Bad Request"
        401 -> "Unauthorized"
        404 -> "Not Found"
        405 -> "Method Not Allowed"
        408 -> "Request Timeout"
        413 -> "Content Too Large"
        417 -> "Expectation Failed"
        431 -> "Request Header Fields Too Large"
        502 -> "Bad Gateway"
        503 -> "Service Unavailable"
        else -> ""
    }
}
