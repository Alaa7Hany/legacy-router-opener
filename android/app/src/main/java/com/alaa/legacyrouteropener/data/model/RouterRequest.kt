package com.alaa.legacyrouteropener.data.model

import com.alaa.legacyrouteropener.data.network.Headers
import com.alaa.legacyrouteropener.data.network.WireBody

internal data class RouterRequest(
    val method: String,
    val path: String,
    val headers: Headers,
    val body: WireBody
)
