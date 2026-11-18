package com.alaa.legacyrouteropener.data.model

data class TlsInfo(
    val target: String,
    val protocol: String,
    val cipherSuite: String? = null,
    val peerSubject: String? = null,
    val peerKeyBits: Int? = null,
    val certNotBefore: Long? = null,
    val certNotAfter: Long? = null,
    val updatedAt: Long = System.currentTimeMillis()
) {
    val certExpired get() = certNotAfter?.let { it < updatedAt } ?: false
}
