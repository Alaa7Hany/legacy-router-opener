package com.alaa.legacyrouteropener

import com.alaa.legacyrouteropener.data.model.Endpoint
import com.alaa.legacyrouteropener.data.model.HardwarePreset
import com.alaa.legacyrouteropener.data.model.RouterRequest
import com.alaa.legacyrouteropener.data.model.RouterResponse
import com.alaa.legacyrouteropener.data.network.*
import com.alaa.legacyrouteropener.domain.server.LocalHttpServer
import com.alaa.legacyrouteropener.domain.server.ResponseTransformer
import org.junit.Assert.*
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.EOFException
import java.net.InetAddress
import java.net.ServerSocket
import java.net.Socket
import java.net.SocketTimeoutException
import java.util.concurrent.CompletableFuture
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.zip.GZIPOutputStream
import kotlin.concurrent.thread

class ProxyWireTest {
    private val hostname = "current-session.localhost"

    @Test fun listenerValidatesAuthorityAndFraming() {
        val budget = MemoryBudget()
        LocalHttpServer(hostname, 0, budget, { request ->
            val body = WireBody(budget)
            body.write("${request.path}:${request.body.inputStream().reader().readText()}".toByteArray())
            RouterResponse(200, mapOf("set-cookie" to listOf("one=1", "two=2")), body)
        }).use { server ->
            server.start()
            val host = "Host: $hostname:${server.port}\r\n"
            val examples = listOf(
                "" to 400, "$host$host" to 400, "Host: old.localhost:${server.port}\r\n" to 404,
                "${host}Content-Length: 0\r\nContent-Length: 1\r\n" to 400,
                "${host}Content-Length: 0\r\nTransfer-Encoding: chunked\r\n" to 400,
                "${host}Transfer-Encoding: gzip, chunked\r\n" to 400,
                "${host}X-Test: ${"x".repeat(8192)}\r\n" to 431,
                "${host}Content-Length: 33554433\r\n" to 413
            )
            for ((headers, status) in examples) {
                val response = send(server.port, "POST / HTTP/1.1\r\n$headers\r\n")
                assertTrue("Expected $status, got $response", response.startsWith("HTTP/1.1 $status"))
            }
            val response = send(server.port, "POST /a%2Fb?q=%2b HTTP/1.1\r\n" +
                "${host}Transfer-Encoding: chunked\r\n\r\n3\r\nabc\r\n2\r\nde\r\n0\r\nX-End: ok\r\n\r\n")
            assertTrue(response.endsWith("/a%2Fb?q=%2b:abcde"))
            assertTrue(response.contains("set-cookie: one=1\r\nset-cookie: two=2\r\n"))
        }
        assertEquals(0, budget.used)
    }

    @Test fun incompleteFramingFailsAndReleasesTheBudget() {
        val budget = MemoryBudget()
        assertThrows(EOFException::class.java) {
            HttpWireCodec.body(ByteArrayInputStream("abc".toByteArray()),
                mapOf("content-length" to listOf("5")), budget, true)
        }
        assertThrows(EOFException::class.java) {
            HttpWireCodec.body(ByteArrayInputStream("3\r\nabc\r\n".toByteArray()),
                mapOf("transfer-encoding" to listOf("chunked")), budget, true)
        }
        assertEquals(0, budget.used)
    }

    @Test fun blockedResponseWriteReleasesTheBudget() {
        val budget = MemoryBudget()
        val ready = CountDownLatch(1)
        LocalHttpServer(hostname, 0, budget, {
            val body = WireBody(budget)
            val data = ByteArray(MIB)
            repeat(16) { body.write(data) }
            ready.countDown()
            RouterResponse(200, emptyMap(), body)
        }, writeTimeoutMillis = 400).use { server ->
            server.start()
            Socket().use { socket ->
                socket.receiveBufferSize = 1024
                socket.connect(java.net.InetSocketAddress("127.0.0.1", server.port))
                socket.getOutputStream().write("GET / HTTP/1.1\r\nHost: $hostname:${server.port}\r\n\r\n".toByteArray())
                assertTrue(ready.await(5, TimeUnit.SECONDS))
                val until = System.nanoTime() + TimeUnit.SECONDS.toNanos(4)
                while (budget.used > 0 && System.nanoTime() < until) Thread.sleep(20)
                assertEquals(0, budget.used)
            }
        }
    }

    @Test fun incompleteHeadersCannotKeepAWorker() {
        val budget = MemoryBudget()
        LocalHttpServer(hostname, 0, budget, { error("Incomplete headers must not dispatch") },
            headerTimeoutMillis = 250).use { server ->
            server.start()
            val response = send(server.port, "GET /")
            assertTrue(response.isEmpty() || response.startsWith("HTTP/1.1 408"))
        }
    }

    @Test fun saturatedListenerRejectsAndRetirementClosesQueuedSockets() {
        val budget = MemoryBudget()
        val sockets = mutableListOf<Socket>()
        LocalHttpServer(hostname, 0, budget, { error("Incomplete headers must not dispatch") }).use { server ->
            server.start()
            try {
                repeat(49) {
                    sockets += Socket("127.0.0.1", server.port).apply { soTimeout = 3000 }
                }
                assertEquals(-1, sockets.last().getInputStream().read())
                server.close()
                for (socket in sockets.dropLast(1)) assertEquals(-1, socket.getInputStream().read())
            } finally {
                sockets.forEach { it.close() }
            }
        }
    }

    @Test fun decompressionAndRewriteChargeOverlappingAllocations() {
        val smaller = MemoryBudget(64 * 1024)
        WireBody(smaller).use { compressed ->
            GZIPOutputStream(object : java.io.OutputStream() {
                override fun write(value: Int) = compressed.write(value)
                override fun write(bytes: ByteArray, off: Int, len: Int) = compressed.write(bytes, off, len)
            }).use { it.write(ByteArray(64 * 1024)) }
            assertThrows(BudgetExceeded::class.java) { HttpWireCodec.decode(compressed, "gzip", smaller) }
            assertEquals(32 * 1024, smaller.used)
        }
        assertEquals(0, smaller.used)
        val budget = MemoryBudget()
        val transformer = ResponseTransformer(Endpoint.parse("192.168.1.1"), "long-session.localhost:8080")
        WireBody(budget).use { original ->
            original.write("http://192.168.1.1/ http://192.168.1.10/".toByteArray())
            transformer.transform(original, "text/html", budget).use {
                assertEquals("http://long-session.localhost:8080/ http://192.168.1.10/",
                    it.inputStream().reader().readText())
            }
        }
        WireBody(budget).use { original ->
            original.write("http://192.168.1.1/ ".repeat(350_000).toByteArray())
            assertThrows(BodyTooLarge::class.java) { transformer.transform(original, "text/html", budget) }
        }
        assertEquals(0, budget.used)
    }

    @Test fun noAutomaticRetryAfterWritingGetOrPost() {
        for (method in listOf("GET", "POST")) {
            val budget = MemoryBudget()
            SocketRegistry().use { sockets ->
                ServerSocket(0, 4, InetAddress.getByName("127.0.0.1")).use { upstream ->
                    val count = CompletableFuture<Int>()
                    thread(isDaemon = true) {
                        try {
                            upstream.accept().use {
                                val reader = it.getInputStream()
                                val head = HttpWireCodec.readRequest(reader)
                                assertEquals(method, head.method)
                                HttpWireCodec.body(reader, head.headers, budget, false).close()
                                it.setSoLinger(true, 0)
                            }
                            upstream.soTimeout = 400
                            try {
                                upstream.accept().close()
                                count.complete(2)
                            } catch (_: SocketTimeoutException) {
                                count.complete(1)
                            }
                        } catch (failure: Throwable) {
                            count.completeExceptionally(failure)
                        }
                    }
                    val client = DirectRouterHttpClient(Endpoint.parse("http://127.0.0.1:${upstream.localPort}"),
                        HardwarePreset.find("auto"), budget, sockets, 8080, "http://test.localhost:8080")
                    WireBody(budget).use { body ->
                        if (method == "POST") body.write("setting=value".toByteArray())
                        assertThrows(java.io.IOException::class.java) {
                            client.send(RouterRequest(method, "/settings", emptyMap(), body), "")
                        }
                    }
                    assertEquals(1, count.get(4, TimeUnit.SECONDS))
                }
            }
            assertEquals(0, budget.used)
        }
    }

    private fun send(port: Int, wire: String): String = Socket("127.0.0.1", port).use {
        it.soTimeout = 3000
        it.getOutputStream().write(wire.toByteArray(Charsets.US_ASCII))
        it.getInputStream().bufferedReader().readText()
    }
}
