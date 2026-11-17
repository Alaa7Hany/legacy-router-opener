package com.alaa.legacyrouteropener.data.network

import android.util.Log
import org.bouncycastle.tls.*
import org.bouncycastle.tls.crypto.DHGroup
import org.bouncycastle.tls.crypto.impl.bc.BcTlsCrypto
import java.security.SecureRandom
import java.util.Hashtable

/**
 * Direct Bouncy Castle TLS Client tuned for legacy hardware firmware
 * (Huawei, ZTE ZXHN, Dell iDRAC 6/7, HPE iLO 2/3, Cisco Catalyst/SG300, APC NMC1/NMC2).
 */
class LegacyRouterTlsClient : DefaultTlsClient(BcTlsCrypto(SecureRandom())) {

    companion object {
        private const val TAG = "LegacyTlsClient"
    }

    override fun getDHGroupVerifier(): TlsDHGroupVerifier {
        // Accept 512-bit and 768-bit Diffie-Hellman primes used by Dell iDRAC 6 and older switches.
        // Bypass DefaultTlsDHGroupVerifier's 2048-bit+ DEFAULT_GROUPS whitelist while keeping the 512-bit floor.
        return object : DefaultTlsDHGroupVerifier(512) {
            override fun checkGroup(dhGroup: DHGroup): Boolean = true
        }
    }

    override fun getProtocolVersions(): Array<ProtocolVersion> {
        val versions = arrayOf(
            ProtocolVersion.TLSv12,
            ProtocolVersion.TLSv11,
            ProtocolVersion.TLSv10,
            ProtocolVersion.SSLv3
        )
        Log.d(TAG, "getProtocolVersions() -> ${versions.map { it.toString() }}")
        return versions
    }

    override fun getCipherSuites(): IntArray {
        val suites = intArrayOf(
            CipherSuite.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
            CipherSuite.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
            CipherSuite.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
            CipherSuite.TLS_ECDHE_RSA_WITH_3DES_EDE_CBC_SHA,
            CipherSuite.TLS_RSA_WITH_AES_256_CBC_SHA256,
            CipherSuite.TLS_RSA_WITH_AES_128_CBC_SHA256,
            CipherSuite.TLS_RSA_WITH_AES_256_CBC_SHA,
            CipherSuite.TLS_RSA_WITH_AES_128_CBC_SHA,
            CipherSuite.TLS_RSA_WITH_3DES_EDE_CBC_SHA,
            CipherSuite.TLS_DHE_RSA_WITH_AES_256_CBC_SHA256,
            CipherSuite.TLS_DHE_RSA_WITH_AES_128_CBC_SHA256,
            CipherSuite.TLS_DHE_RSA_WITH_AES_256_CBC_SHA,
            CipherSuite.TLS_DHE_RSA_WITH_AES_128_CBC_SHA,
            CipherSuite.TLS_DHE_RSA_WITH_3DES_EDE_CBC_SHA,
            CipherSuite.TLS_DHE_DSS_WITH_AES_256_CBC_SHA256,
            CipherSuite.TLS_DHE_DSS_WITH_AES_128_CBC_SHA256,
            CipherSuite.TLS_DHE_DSS_WITH_AES_256_CBC_SHA,
            CipherSuite.TLS_DHE_DSS_WITH_AES_128_CBC_SHA,
            CipherSuite.TLS_DHE_DSS_WITH_3DES_EDE_CBC_SHA,
            CipherSuite.TLS_EMPTY_RENEGOTIATION_INFO_SCSV
        )
        Log.d(TAG, "getCipherSuites() -> ${suites.size} suites: ${suites.map { "0x${it.toString(16).padStart(4, '0')}" }}")
        return suites
    }

    override fun getAuthentication(): TlsAuthentication {
        Log.d(TAG, "getAuthentication() called")
        return object : TlsAuthentication {
            override fun notifyServerCertificate(serverCertificate: TlsServerCertificate?) {
                Log.d(TAG, "notifyServerCertificate() -> Accepting certificate (trust-all)")
            }

            override fun getClientCredentials(certificateRequest: CertificateRequest?): TlsCredentials? {
                Log.d(TAG, "getClientCredentials() -> null (no client cert)")
                return null
            }
        }
    }

    @Suppress("UNCHECKED_CAST")
    override fun getClientExtensions(): Hashtable<*, *>? {
        val extensions = super.getClientExtensions() as? Hashtable<Int, ByteArray>

        if (extensions == null) {
            Log.w(TAG, "getClientExtensions() -> super returned null, returning empty Hashtable")
            return Hashtable<Int, ByteArray>()
        }

        Log.d(TAG, "getClientExtensions() -> super returned ${extensions.size} extensions: ${extensions.keys.toList().map { "0x${it.toString(16)}" }}")

        // Remove extensions that confuse legacy firmware (keep signature_algorithms for TLS 1.2 DHE/ECDHE)
        val stripExtensions = setOf(
            ExtensionType.server_name,                // 0 - SNI
            ExtensionType.extended_master_secret,     // 23 - EMS
            ExtensionType.supported_versions,         // 43
        )
        for (ext in stripExtensions) {
            if (extensions.containsKey(ext)) {
                extensions.remove(ext)
                Log.d(TAG, "  Stripped extension 0x${ext.toString(16)}")
            }
        }

        Log.d(TAG, "getClientExtensions() -> final ${extensions.size} extensions: ${extensions.keys.toList().map { "0x${it.toString(16)}" }}")
        return extensions
    }

    override fun notifySecureRenegotiation(secureRenegotiation: Boolean) {
        // Accept legacy servers that don't support RFC 5746 secure renegotiation.
        // Equivalent to OpenSSL's SSL_OP_LEGACY_SERVER_CONNECT flag.
        Log.d(TAG, "notifySecureRenegotiation() -> secureRenegotiation=$secureRenegotiation (accepting regardless)")
    }

    override fun notifyServerVersion(serverVersion: ProtocolVersion?) {
        Log.i(TAG, "notifyServerVersion() -> Server selected: $serverVersion")
        super.notifyServerVersion(serverVersion)
    }

    override fun notifySelectedCipherSuite(selectedCipherSuite: Int) {
        Log.i(TAG, "notifySelectedCipherSuite() -> Server selected: 0x${selectedCipherSuite.toString(16).padStart(4, '0')}")
        super.notifySelectedCipherSuite(selectedCipherSuite)
    }

    override fun notifyAlertReceived(alertLevel: Short, alertDescription: Short) {
        val levelStr = if (alertLevel.toInt() == 1) "WARNING" else "FATAL"
        Log.e(TAG, "notifyAlertReceived() -> $levelStr alert: level=$alertLevel description=$alertDescription")
        super.notifyAlertReceived(alertLevel, alertDescription)
    }

    override fun notifyHandshakeComplete() {
        Log.i(TAG, "notifyHandshakeComplete() -> TLS handshake completed successfully!")
        super.notifyHandshakeComplete()
    }
}
