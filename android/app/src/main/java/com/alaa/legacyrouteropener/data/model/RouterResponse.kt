package com.alaa.legacyrouteropener.data.model

data class RouterResponse(
    val statusCode: Int,
    val statusText: String,
    val headers: Map<String, List<String>>,
    val body: ByteArray
) {
    val isText: Boolean
        get() {
            val contentType = headers.entries.firstOrNull { it.key.equals("Content-Type", ignoreCase = true) }?.value?.firstOrNull() ?: ""
            return contentType.contains("html", ignoreCase = true) ||
                    contentType.contains("javascript", ignoreCase = true) ||
                    contentType.contains("json", ignoreCase = true) ||
                    contentType.contains("text", ignoreCase = true)
        }

    val contentType: String
        get() = headers.entries.firstOrNull { it.key.equals("Content-Type", ignoreCase = true) }?.value?.firstOrNull() ?: "text/html; charset=utf-8"

    override fun equals(other: Any?): Boolean {
        if (this === other) return true
        if (javaClass != other?.javaClass) return false

        other as RouterResponse

        if (statusCode != other.statusCode) return false
        if (statusText != other.statusText) return false
        if (headers != other.headers) return false
        if (!body.contentEquals(other.body)) return false

        return true
    }

    override fun hashCode(): Int {
        var result = statusCode
        result = 31 * result + statusText.hashCode()
        result = 31 * result + headers.hashCode()
        result = 31 * result + body.contentHashCode()
        return result
    }
}
