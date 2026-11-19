package com.alaa.legacyrouteropener.data.network

import org.bouncycastle.crypto.engines.RC4Engine
import org.bouncycastle.crypto.params.KeyParameter
import org.bouncycastle.tls.AlertDescription
import org.bouncycastle.tls.EncryptionAlgorithm
import org.bouncycastle.tls.ProtocolVersion
import org.bouncycastle.tls.TlsFatalAlert
import org.bouncycastle.tls.crypto.TlsCipher
import org.bouncycastle.tls.crypto.TlsCryptoParameters
import org.bouncycastle.tls.crypto.TlsDecodeResult
import org.bouncycastle.tls.crypto.TlsEncodeResult
import org.bouncycastle.tls.crypto.impl.TlsImplUtils
import org.bouncycastle.tls.crypto.impl.TlsSuiteHMac
import org.bouncycastle.tls.crypto.impl.bc.BcTlsCrypto
import org.bouncycastle.util.Arrays
import java.security.SecureRandom

internal class LegacyTlsCrypto : BcTlsCrypto(SecureRandom()) {
    override fun hasEncryptionAlgorithm(algorithm: Int) =
        algorithm == EncryptionAlgorithm.RC4_128 || super.hasEncryptionAlgorithm(algorithm)

    override fun createCipher(parameters: TlsCryptoParameters, encryption: Int, mac: Int): TlsCipher {
        if (encryption != EncryptionAlgorithm.RC4_128) return super.createCipher(parameters, encryption, mac)
        val clientMac = createMAC(parameters, mac)
        val serverMac = createMAC(parameters, mac)
        val keySize = 16
        val macSize = clientMac.macLength
        val material = TlsImplUtils.calculateKeyBlock(parameters, 2 * (macSize + keySize))
        try {
            clientMac.setKey(material, 0, macSize)
            serverMac.setKey(material, macSize, macSize)
            val client = RC4Engine().apply { init(true, KeyParameter(material, 2 * macSize, keySize)) }
            val server = RC4Engine().apply { init(true, KeyParameter(material, 2 * macSize + keySize, keySize)) }
            return if (parameters.isServer) {
                Rc4RecordCipher(server, client, TlsSuiteHMac(parameters, serverMac), TlsSuiteHMac(parameters, clientMac))
            } else {
                Rc4RecordCipher(client, server, TlsSuiteHMac(parameters, clientMac), TlsSuiteHMac(parameters, serverMac))
            }
        } finally {
            material.fill(0)
        }
    }

    companion object {
        val shared = LegacyTlsCrypto()
    }
}

private class Rc4RecordCipher(
    private val encoder: RC4Engine,
    private val decoder: RC4Engine,
    private val writeMac: TlsSuiteHMac,
    private val readMac: TlsSuiteHMac
) : TlsCipher {
    override fun getCiphertextDecodeLimit(limit: Int) = limit + readMac.size
    override fun getCiphertextEncodeLimit(limit: Int) = limit + writeMac.size
    override fun getPlaintextDecodeLimit(limit: Int) = limit - readMac.size
    override fun getPlaintextEncodeLimit(limit: Int) = limit - writeMac.size

    @Synchronized
    override fun encodePlaintext(sequence: Long, type: Short, version: ProtocolVersion, header: Int,
        bytes: ByteArray, offset: Int, length: Int): TlsEncodeResult {
        val tag = writeMac.calculateMac(sequence, type, null, bytes, offset, length)
        val output = ByteArray(header + length + tag.size)
        encoder.processBytes(bytes, offset, length, output, header)
        encoder.processBytes(tag, 0, tag.size, output, header + length)
        return TlsEncodeResult(output, 0, output.size, type)
    }

    @Synchronized
    override fun decodeCiphertext(sequence: Long, type: Short, version: ProtocolVersion,
        bytes: ByteArray, offset: Int, length: Int): TlsDecodeResult {
        val plaintextLength = getPlaintextDecodeLimit(length)
        if (plaintextLength < 0) throw TlsFatalAlert(AlertDescription.decode_error)
        decoder.processBytes(bytes, offset, length, bytes, offset)
        val tag = readMac.calculateMac(sequence, type, null, bytes, offset, plaintextLength)
        if (!Arrays.constantTimeAreEqual(tag.size, tag, 0, bytes, offset + plaintextLength)) {
            throw TlsFatalAlert(AlertDescription.bad_record_mac)
        }
        return TlsDecodeResult(bytes, offset, plaintextLength, type)
    }

    override fun rekeyDecoder() = throw TlsFatalAlert(AlertDescription.internal_error)
    override fun rekeyEncoder() = throw TlsFatalAlert(AlertDescription.internal_error)
    override fun usesOpaqueRecordTypeDecode() = false
    override fun usesOpaqueRecordTypeEncode() = false
}
