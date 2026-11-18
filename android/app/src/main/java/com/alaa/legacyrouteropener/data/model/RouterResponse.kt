package com.alaa.legacyrouteropener.data.model

import com.alaa.legacyrouteropener.data.network.Headers
import com.alaa.legacyrouteropener.data.network.WireBody
import java.io.Closeable

internal class RouterResponse(val status: Int, val headers: Headers, val body: WireBody) : Closeable {
    override fun close() = body.close()
}
