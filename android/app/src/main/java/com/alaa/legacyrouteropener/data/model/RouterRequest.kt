package com.alaa.legacyrouteropener.data.model

data class RouterRequest(
    val method: String,
    val path: String,
    val headers: Map<String, String>,
    val body: ByteArray? = null
) {
    override fun equals(other: Any?): Boolean {
        if (this === other) return true
        if (javaClass != other?.javaClass) return false

        other as RouterRequest

        if (method != other.method) return false
        if (path != other.path) return false
        if (headers != other.headers) return false
        if (body != null) {
            if (other.body == null) return false
            if (!body.contentEquals(other.body)) return false
        } else if (other.body != null) return false

        return true
    }

    override fun hashCode(): Int {
        var result = method.hashCode()
        result = 31 * result + path.hashCode()
        result = 31 * result + headers.hashCode()
        result = 31 * result + (body?.contentHashCode() ?: 0)
        return result
    }
}
