package com.alaa.legacyrouteropener.data.network


internal data class RouterRequest(
    val method: String,
    val path: String,
    val headers: Headers,
    val body: WireBody
)
