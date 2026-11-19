package com.alaa.legacyrouteropener.data.network

import java.io.Closeable

internal class RouterResponse(val status: Int, val headers: Headers, val body: WireBody) : Closeable {
    override fun close() = body.close()
}
