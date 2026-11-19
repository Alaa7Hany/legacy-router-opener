package com.alaa.legacyrouteropener.data.network

import com.alaa.legacyrouteropener.domain.model.Endpoint
import com.alaa.legacyrouteropener.domain.model.TlsInfo
import org.bouncycastle.tls.*
import org.bouncycastle.tls.crypto.DHGroup
import java.io.ByteArrayInputStream
import java.security.cert.CertificateFactory
import java.security.cert.X509Certificate
import java.security.interfaces.DSAKey
import java.security.interfaces.ECKey
import java.security.interfaces.RSAKey
import java.util.Hashtable

internal enum class TlsProfile { MODERN, LEGACY_STRIPPED, SSLV3_ONLY }

internal class LegacyRouterTlsClient(
    private val endpoint: Endpoint,
    private val profile: TlsProfile
) : DefaultTlsClient(LegacyTlsCrypto.shared) {
    private var certificate: X509Certificate? = null

    override fun getProtocolVersions(): Array<ProtocolVersion> = when (profile) {
        TlsProfile.MODERN -> arrayOf(ProtocolVersion.TLSv13, ProtocolVersion.TLSv12)
        TlsProfile.LEGACY_STRIPPED -> arrayOf(ProtocolVersion.TLSv12, ProtocolVersion.TLSv11, ProtocolVersion.TLSv10)
        TlsProfile.SSLV3_ONLY -> arrayOf(ProtocolVersion.SSLv3)
    }

    override fun getCipherSuites(): IntArray = when (profile) {
        TlsProfile.MODERN -> (super.getCipherSuites() + intArrayOf(
            CipherSuite.TLS_RSA_WITH_AES_128_GCM_SHA256,
            CipherSuite.TLS_RSA_WITH_AES_256_CBC_SHA,
            CipherSuite.TLS_RSA_WITH_AES_128_CBC_SHA
        )).distinct().toIntArray()
        TlsProfile.LEGACY_STRIPPED -> legacySuites
        TlsProfile.SSLV3_ONLY -> intArrayOf(
            CipherSuite.TLS_RSA_WITH_3DES_EDE_CBC_SHA,
            CipherSuite.TLS_DHE_RSA_WITH_3DES_EDE_CBC_SHA,
            CipherSuite.TLS_RSA_WITH_RC4_128_SHA,
            CipherSuite.TLS_RSA_WITH_RC4_128_MD5
        )
    }

    override fun getDHGroupVerifier(): TlsDHGroupVerifier {
        if (profile == TlsProfile.MODERN) return super.getDHGroupVerifier()
        return object : DefaultTlsDHGroupVerifier(512) {
            override fun checkGroup(dhGroup: DHGroup): Boolean = true
        }
    }

    override fun shouldUseExtendedMasterSecret() = profile == TlsProfile.MODERN
    override fun requiresCloseNotify() = false
    override fun getRenegotiationPolicy() =
        if (profile == TlsProfile.MODERN) RenegotiationPolicy.DENY else RenegotiationPolicy.ACCEPT

    override fun notifySecureRenegotiation(secureRenegotiation: Boolean) {
        if (profile == TlsProfile.MODERN) super.notifySecureRenegotiation(secureRenegotiation)
    }

    @Suppress("UNCHECKED_CAST")
    override fun getClientExtensions(): Hashtable<Any, Any> {
        val extensions = super.getClientExtensions() as? Hashtable<Any, Any> ?: Hashtable()
        if (profile != TlsProfile.MODERN) extensions.remove(ExtensionType.server_name)
        return extensions
    }

    override fun getSNIServerNames(): java.util.Vector<ServerName>? {
        if (profile != TlsProfile.MODERN || ':' in endpoint.host || endpoint.host.all { it.isDigit() || it == '.' }) {
            return null
        }
        return java.util.Vector<ServerName>().apply {
            add(ServerName(NameType.host_name, endpoint.host.toByteArray(Charsets.US_ASCII)))
        }
    }

    override fun getAuthentication(): TlsAuthentication = object : TlsAuthentication {
        override fun notifyServerCertificate(serverCertificate: TlsServerCertificate) {
            val chain = serverCertificate.certificate
            if (chain.isEmpty) throw TlsFatalAlert(AlertDescription.bad_certificate)
            // Device certificates are accepted for compatibility. The handshake still proves key possession.
            certificate = CertificateFactory.getInstance("X.509")
                .generateCertificate(ByteArrayInputStream(chain.getCertificateAt(0).encoded)) as X509Certificate
        }

        override fun getClientCredentials(request: CertificateRequest): TlsCredentials? = null
    }

    fun info(): TlsInfo {
        val security = context.securityParametersConnection
        val bits = when (val key = certificate?.publicKey) {
            is RSAKey -> key.modulus.bitLength()
            is ECKey -> key.params.order.bitLength()
            is DSAKey -> key.params.p.bitLength()
            else -> null
        }
        return TlsInfo(endpoint.authority, security.negotiatedVersion.toString(), cipherNames[security.cipherSuite],
            certificate?.subjectX500Principal?.name, bits, certificate?.notBefore?.time, certificate?.notAfter?.time)
    }

    companion object {
        private val legacySuites = intArrayOf(
            CipherSuite.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
            CipherSuite.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
            CipherSuite.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA,
            CipherSuite.TLS_RSA_WITH_AES_256_CBC_SHA256,
            CipherSuite.TLS_RSA_WITH_AES_128_CBC_SHA256,
            CipherSuite.TLS_RSA_WITH_AES_256_CBC_SHA,
            CipherSuite.TLS_RSA_WITH_AES_128_CBC_SHA,
            CipherSuite.TLS_RSA_WITH_3DES_EDE_CBC_SHA,
            CipherSuite.TLS_DHE_RSA_WITH_AES_256_CBC_SHA,
            CipherSuite.TLS_DHE_RSA_WITH_AES_128_CBC_SHA,
            CipherSuite.TLS_DHE_RSA_WITH_3DES_EDE_CBC_SHA,
            CipherSuite.TLS_DHE_DSS_WITH_AES_256_CBC_SHA,
            CipherSuite.TLS_DHE_DSS_WITH_AES_128_CBC_SHA,
            CipherSuite.TLS_DHE_DSS_WITH_3DES_EDE_CBC_SHA,
            CipherSuite.TLS_RSA_WITH_RC4_128_SHA,
            CipherSuite.TLS_RSA_WITH_RC4_128_MD5
        )
        private val cipherNames = CipherSuite::class.java.fields
            .filter { it.type == Int::class.javaPrimitiveType && it.name.startsWith("TLS_") }
            .associate { it.getInt(null) to it.name }
    }
}
