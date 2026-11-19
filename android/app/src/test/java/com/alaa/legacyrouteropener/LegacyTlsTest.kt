package com.alaa.legacyrouteropener

import com.alaa.legacyrouteropener.domain.model.Endpoint
import com.alaa.legacyrouteropener.domain.model.HardwarePreset
import com.alaa.legacyrouteropener.data.network.RouterRequest
import com.alaa.legacyrouteropener.data.network.*
import org.bouncycastle.asn1.x500.X500Name
import org.bouncycastle.cert.jcajce.JcaX509v3CertificateBuilder
import org.bouncycastle.crypto.util.PrivateKeyFactory
import org.bouncycastle.operator.jcajce.JcaContentSignerBuilder
import org.bouncycastle.tls.*
import org.bouncycastle.tls.crypto.impl.bc.BcDefaultTlsCredentialedDecryptor
import org.bouncycastle.tls.crypto.impl.bc.BcDefaultTlsCredentialedSigner
import org.bouncycastle.tls.crypto.TlsCryptoParameters
import org.bouncycastle.tls.crypto.DHGroup
import org.bouncycastle.tls.crypto.TlsDHConfig
import org.junit.Assert.*
import org.junit.Test
import java.io.IOException
import java.math.BigInteger
import java.net.InetAddress
import java.net.ServerSocket
import java.security.KeyPairGenerator
import java.security.AlgorithmParameterGenerator
import java.util.Date
import java.util.concurrent.CompletableFuture
import java.util.concurrent.TimeUnit
import kotlin.concurrent.thread
import javax.crypto.spec.DHParameterSpec

class LegacyTlsTest {
    @Test fun negotiatedProtocolsAndLegacyCiphers() {
        val cases = listOf(
            ProtocolVersion.TLSv12 to CipherSuite.TLS_RSA_WITH_AES_128_CBC_SHA,
            ProtocolVersion.TLSv12 to CipherSuite.TLS_DHE_RSA_WITH_AES_128_CBC_SHA,
            ProtocolVersion.TLSv11 to CipherSuite.TLS_RSA_WITH_AES_128_CBC_SHA,
            ProtocolVersion.TLSv10 to CipherSuite.TLS_RSA_WITH_RC4_128_SHA,
            ProtocolVersion.SSLv3 to CipherSuite.TLS_RSA_WITH_RC4_128_MD5
        )
        cases.forEach { (version, suite) ->
            try {
                exercise(version, suite, "Content-Length: 2\r\n", "OK", false) { client, request ->
                    val result = client.send(request, "")
                    result.response.use { response ->
                        assertEquals("OK", response.body.inputStream().reader().readText())
                        assertEquals(version.toString(), result.tlsInfo.protocol)
                        assertEquals("CN=Test Router", result.tlsInfo.peerSubject)
                        assertEquals(2048, result.tlsInfo.peerKeyBits)
                    }
                }
            } catch (failure: Exception) {
                throw AssertionError("Fixture negotiation failed for $version and suite $suite", failure)
            }
        }
    }

    @Test fun closeDelimitedTlsRequiresCloseNotify() {
        exercise(ProtocolVersion.TLSv12, CipherSuite.TLS_RSA_WITH_AES_128_CBC_SHA,
            "", "OK", false) { client, request ->
            assertThrows(IOException::class.java) { client.send(request, "") }
        }
        exercise(ProtocolVersion.TLSv12, CipherSuite.TLS_RSA_WITH_AES_128_CBC_SHA,
            "", "OK", true) { client, request ->
            client.send(request, "").response.use {
                assertEquals("OK", it.body.inputStream().reader().readText())
            }
        }
    }

    private fun exercise(version: ProtocolVersion, suite: Int, headers: String, body: String,
        closeNotify: Boolean, assertion: (DirectRouterHttpClient, RouterRequest) -> Unit) {
        ServerSocket(0, 4, InetAddress.getByName("127.0.0.1")).use { fixture ->
            val completion = CompletableFuture<Unit>()
            thread(isDaemon = true) {
                try {
                    // Earlier profiles can fail before a byte of HTTP is sent.
                    val attempts = mutableListOf<IOException>()
                    repeat(3) {
                        var accepted = false
                        fixture.accept().use { socket ->
                            socket.soTimeout = 4000
                            val tls = TlsServerProtocol(socket.getInputStream(), socket.getOutputStream())
                            try {
                                tls.accept(testServer(version, suite))
                                accepted = true
                                HttpWireCodec.readRequest(tls.inputStream)
                                tls.outputStream.write("HTTP/1.0 200 OK\r\n${headers}\r\n$body".toByteArray())
                                tls.outputStream.flush()
                                if (closeNotify) tls.close()
                            } catch (failure: IOException) {
                                if (accepted) throw failure
                                attempts.add(failure)
                            }
                        }
                        if (accepted) {
                            completion.complete(Unit)
                            return@thread
                        }
                    }
                    throw IOException("No requested TLS profile completed").apply { attempts.forEach(::addSuppressed) }
                } catch (failure: Throwable) {
                    completion.completeExceptionally(failure)
                }
            }
            val budget = MemoryBudget()
            SocketRegistry().use { sockets ->
                val client = DirectRouterHttpClient(
                    Endpoint.parse("https://127.0.0.1:${fixture.localPort}"),
                    HardwarePreset.find("auto"), budget, sockets, 8080, "http://test.localhost:8080")
                WireBody(budget).use { request ->
                    try {
                        assertion(client, RouterRequest("GET", "/", emptyMap(), request))
                    } catch (failure: Exception) {
                        runCatching { completion.get(250, TimeUnit.MILLISECONDS) }.exceptionOrNull()?.let {
                            failure.addSuppressed(it)
                        }
                        throw failure
                    }
                }
            }
            completion.get(5, TimeUnit.SECONDS)
            assertEquals(0, budget.used)
        }
    }

    companion object {
        private val keys = KeyPairGenerator.getInstance("RSA").apply { initialize(2048) }.generateKeyPair()
        private val dh by lazy {
            val parameters = AlgorithmParameterGenerator.getInstance("DiffieHellman").apply { init(512) }
                .generateParameters().getParameterSpec(DHParameterSpec::class.java)
            TlsDHConfig(DHGroup(parameters.p, null, parameters.g, 0))
        }
        private val certificate = X500Name("CN=Test Router").let { name ->
            JcaX509v3CertificateBuilder(name, BigInteger.ONE,
                Date(System.currentTimeMillis() - 60_000L), Date(System.currentTimeMillis() + 60_000L),
                name, keys.public
            ).build(JcaContentSignerBuilder("SHA256withRSA").build(keys.private)).encoded
        }

        internal fun testServer(version: ProtocolVersion, suite: Int): DefaultTlsServer {
            val crypto = LegacyTlsCrypto()
            val chain = Certificate(arrayOf(crypto.createCertificate(certificate)))
            val key = PrivateKeyFactory.createKey(keys.private.encoded)
            return object : DefaultTlsServer(crypto) {
                override fun getProtocolVersions() = arrayOf(version)
                override fun getCipherSuites() = intArrayOf(suite)
                override fun getDHConfig() = dh
                override fun getRSAEncryptionCredentials() = BcDefaultTlsCredentialedDecryptor(crypto, chain, key)
                override fun getRSASignerCredentials() = BcDefaultTlsCredentialedSigner(
                    TlsCryptoParameters(context), crypto, key, chain,
                    if (version == ProtocolVersion.TLSv12) SignatureAndHashAlgorithm(HashAlgorithm.sha256, SignatureAlgorithm.rsa)
                    else null
                )
            }
        }
    }
}
