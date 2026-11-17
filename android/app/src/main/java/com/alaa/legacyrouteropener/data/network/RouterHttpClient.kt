package com.alaa.legacyrouteropener.data.network

import com.alaa.legacyrouteropener.data.model.RouterRequest
import com.alaa.legacyrouteropener.data.model.RouterResponse

interface RouterHttpClient {
    fun sendRequest(
        routerIp: String,
        request: RouterRequest,
        cookieHeader: String?
    ): RouterResponse
}
