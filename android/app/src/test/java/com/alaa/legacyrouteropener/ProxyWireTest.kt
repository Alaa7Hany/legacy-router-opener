package com.alaa.legacyrouteropener

import com.alaa.legacyrouteropener.domain.model.AppLanguage
import com.alaa.legacyrouteropener.domain.model.Endpoint
import com.alaa.legacyrouteropener.domain.model.HardwarePreset
import com.alaa.legacyrouteropener.data.network.RouterRequest
import com.alaa.legacyrouteropener.data.network.RouterResponse
import com.alaa.legacyrouteropener.domain.model.TlsInfo
import com.alaa.legacyrouteropener.data.network.*
import com.alaa.legacyrouteropener.data.repository.SessionCookieJar
import com.alaa.legacyrouteropener.data.proxy.LocalHttpServer
import com.alaa.legacyrouteropener.data.proxy.ProxyServer
import com.alaa.legacyrouteropener.data.proxy.ResponseTransformer
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
        LocalHttpServer(hostname, 0, budget).use { server ->
            server.start { request ->
                val body = WireBody(budget)
                body.write("${request.path}:${request.body.inputStream().reader().readText()}".toByteArray())
                RouterResponse(200, mapOf("set-cookie" to listOf("one=1", "two=2")), body)
            }
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
        LocalHttpServer(hostname, 0, budget, writeTimeoutMillis = 400).use { server ->
            server.start {
                val body = WireBody(budget)
                val data = ByteArray(MIB)
                repeat(16) { body.write(data) }
                ready.countDown()
                RouterResponse(200, emptyMap(), body)
            }
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
        LocalHttpServer(hostname, 0, budget, headerTimeoutMillis = 250).use { server ->
            server.start { error("Incomplete headers must not dispatch") }
            val response = send(server.port, "GET /")
            assertTrue(response.isEmpty() || response.startsWith("HTTP/1.1 408"))
        }
    }

    @Test fun saturatedListenerRejectsAndRetirementClosesQueuedSockets() {
        val budget = MemoryBudget()
        val sockets = mutableListOf<Socket>()
        LocalHttpServer(hostname, 0, budget).use { server ->
            server.start { error("Incomplete headers must not dispatch") }
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
            GZIPOutputStream(compressed.asOutputStream()).use { it.write(ByteArray(64 * 1024)) }
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

    @Test fun keepAliveRedirectWithoutContentLengthDoesNotHang() {
        val budget = MemoryBudget()
        SocketRegistry().use { sockets ->
            ServerSocket(0, 4, InetAddress.getByName("127.0.0.1")).use { upstream ->
                val done = CountDownLatch(1)
                thread(isDaemon = true) {
                    upstream.accept().use { socket ->
                        val head = HttpWireCodec.readRequest(socket.getInputStream())
                        HttpWireCodec.body(socket.getInputStream(), head.headers, budget, false).close()
                        val reply = "HTTP/1.1 302 Found\r\n" +
                            "Connection: Keep-Alive\r\n" +
                            "Location: /html/content.asp\r\n" +
                            "Set-Cookie: SessionID_R3=tok123; Expires=Sun, 02 Jan 2000 22:00:00 GMT\r\n\r\n"
                        socket.getOutputStream().write(reply.toByteArray(Charsets.ISO_8859_1))
                        socket.getOutputStream().flush()
                        // Hold socket open like Huawei Keep-Alive servers do.
                        done.await(5, TimeUnit.SECONDS)
                    }
                }
                val client = DirectRouterHttpClient(
                    Endpoint.parse("http://127.0.0.1:${upstream.localPort}"),
                    HardwarePreset.find("auto"), budget, sockets, 8080, "http://test.localhost:8080"
                )
                WireBody(budget).use { reqBody ->
                    reqBody.write("Username=admin".toByteArray())
                    val started = System.nanoTime()
                    client.send(RouterRequest("POST", "/index/login.cgi", emptyMap(), reqBody), "").response.use { res ->
                        val elapsedMs = TimeUnit.NANOSECONDS.toMillis(System.nanoTime() - started)
                        assertEquals(302, res.status)
                        assertEquals("/html/content.asp", res.headers.first("location"))
                        assertTrue("Expected immediate redirect, took ${elapsedMs}ms", elapsedMs < 3000)
                    }
                }
                done.countDown()
            }
        }
        assertEquals(0, budget.used)
    }

    @Test fun legacyRouterCookiesSurviveSubpathsAndOldClock() {
        val jar = SessionCookieJar(HardwarePreset.find("auto").seedCookies)
        jar.save(listOf("SessionID_R3=abc123; Expires=Sun, 02 Jan 2000 22:00:00 GMT; Path=/index; Secure; HttpOnly"))
        val merged = jar.merge(emptyList())
        assertTrue(merged.contains("SessionID_R3=abc123"))
        assertTrue(merged.contains("FirstMenu=Admin_0"))

        val cleaned = ResponseTransformer.cleanCookie(
            "SessionID_R3=abc123; Expires=Sun, 02 Jan 2000 22:00:00 GMT; Path=/index; Secure; HttpOnly; SameSite=None"
        )
        assertTrue(cleaned.contains("SessionID_R3=abc123"))
        assertTrue(cleaned.contains("Path=/"))
        assertTrue(cleaned.contains("SameSite=Lax"))
        assertFalse(cleaned.contains("Secure", ignoreCase = true))
        assertFalse(cleaned.contains("Expires=", ignoreCase = true))
    }

    @Test fun staticCacheAccountsForMemoryBudgetAndReleasesOnClose() {
        val budget = MemoryBudget()
        val endpoint = Endpoint.parse("http://192.168.1.1")
        var upstreamCalls = 0
        val upstream = object : RouterHttpClient {
            override fun send(request: RouterRequest, cookies: String): UpstreamResult {
                upstreamCalls++
                val body = WireBody(budget).apply { write("body { color: #000; }".toByteArray()) }
                return UpstreamResult(
                    RouterResponse(200, mapOf("content-type" to listOf("text/css")), body),
                    endpoint,
                    TlsInfo(endpoint.authority, "HTTP")
                )
            }
        }
        ProxyServer(
            endpoint = endpoint,
            authority = "session.localhost:8080",
            preset = HardwarePreset.find("auto"),
            budget = budget,
            upstream = upstream,
            language = { AppLanguage.EN },
            message = { _, key -> key },
            telemetry = {},
            log = {}
        ).use { server ->
            WireBody(budget).use { reqBody ->
                server.serve(RouterRequest("GET", "/css/main.css", emptyMap(), reqBody)).use { res ->
                    assertEquals(200, res.status)
                }
            }
            assertEquals(1, upstreamCalls)
            assertTrue("Cached asset should remain charged to MemoryBudget", budget.used > 0)

            WireBody(budget).use { reqBody ->
                server.serve(RouterRequest("GET", "/css/main.css", emptyMap(), reqBody)).use { res ->
                    assertEquals(200, res.status)
                }
            }
            assertEquals(1, upstreamCalls)
        }
        assertEquals(0, budget.used)
    }

    @Test fun directClientFallsBackToSecondResolvedAddress() {
        val budget = MemoryBudget()
        SocketRegistry().use { sockets ->
            ServerSocket(0, 4, InetAddress.getByName("127.0.0.1")).use { upstream ->
                val done = CountDownLatch(1)
                thread(isDaemon = true) {
                    upstream.accept().use { socket ->
                        val head = HttpWireCodec.readRequest(socket.getInputStream())
                        HttpWireCodec.body(socket.getInputStream(), head.headers, budget, false).close()
                        val reply = "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK"
                        socket.getOutputStream().write(reply.toByteArray(Charsets.ISO_8859_1))
                        socket.getOutputStream().flush()
                        done.countDown()
                    }
                }
                val deadLoopback = InetAddress.getByAddress("router.local", byteArrayOf(127, 0, 0, 2))
                val liveLoopback = InetAddress.getByName("127.0.0.1")
                val client = DirectRouterHttpClient(
                    target = Endpoint.parse("http://router.local:${upstream.localPort}"),
                    preset = HardwarePreset.find("auto"),
                    budget = budget,
                    sockets = sockets,
                    blockedPort = 8080,
                    origin = "http://test.localhost:8080",
                    dnsResolver = { arrayOf(deadLoopback, liveLoopback) }
                )
                WireBody(budget).use { reqBody ->
                    client.send(RouterRequest("GET", "/", emptyMap(), reqBody), "").response.use { res ->
                        assertEquals(200, res.status)
                        assertEquals("OK", res.body.inputStream().reader().readText())
                    }
                }
                assertTrue(done.await(3, TimeUnit.SECONDS))
            }
        }
        assertEquals(0, budget.used)
    }

    private fun send(port: Int, wire: String): String = Socket("127.0.0.1", port).use {
        it.soTimeout = 3000
        it.getOutputStream().write(wire.toByteArray(Charsets.US_ASCII))
        it.getInputStream().bufferedReader().readText()
    }
}
