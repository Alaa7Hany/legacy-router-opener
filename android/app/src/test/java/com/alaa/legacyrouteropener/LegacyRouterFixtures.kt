package com.alaa.legacyrouteropener

import com.alaa.legacyrouteropener.data.network.HttpWireCodec
import org.bouncycastle.tls.CipherSuite
import org.bouncycastle.tls.ProtocolVersion
import org.bouncycastle.tls.TlsServerProtocol
import java.io.IOException
import java.net.InetAddress
import java.net.ServerSocket
import kotlin.concurrent.thread

object LegacyRouterFixtures {
    @JvmStatic
    fun main(args: Array<String>) {
        val suites = listOf(
            Triple("TLS12", ProtocolVersion.TLSv12, CipherSuite.TLS_RSA_WITH_AES_128_CBC_SHA),
            Triple("TLS10", ProtocolVersion.TLSv10, CipherSuite.TLS_RSA_WITH_AES_128_CBC_SHA),
            Triple("RC4", ProtocolVersion.TLSv10, CipherSuite.TLS_RSA_WITH_RC4_128_SHA),
            Triple("SSL3", ProtocolVersion.SSLv3, CipherSuite.TLS_RSA_WITH_RC4_128_MD5)
        )
        val fixtures = suites.map { (name, version, suite) ->
            val listener = ServerSocket(0, 8, InetAddress.getByName("127.0.0.1"))
            println("$name=${listener.localPort}")
            thread(name = "fixture-$name") {
                listener.use {
                    while (true) {
                        listener.accept().use { client ->
                            try {
                                client.soTimeout = 5000
                                val tls = TlsServerProtocol(client.getInputStream(), client.getOutputStream())
                                tls.accept(LegacyTlsTest.testServer(version, suite))
                                val request = HttpWireCodec.readRequest(tls.inputStream)
                                val body = "<!doctype html><title>$name fixture</title><h1>$name fixture ready</h1>"
                                    .toByteArray()
                                HttpWireCodec.writeHead(tls.outputStream, "HTTP/1.1 200 OK", mapOf(
                                    "content-type" to listOf("text/html; charset=utf-8"),
                                    "content-length" to listOf(body.size.toString())
                                ))
                                if (request.method != "HEAD") tls.outputStream.write(body)
                                tls.outputStream.flush()
                                println("$name served ${request.method} ${request.path.substringBefore('?')}")
                            } catch (_: IOException) {
                                // A compatibility profile probe can close before any HTTP request.
                            }
                        }
                    }
                }
            }
        }
        fixtures.forEach(Thread::join)
    }
}
