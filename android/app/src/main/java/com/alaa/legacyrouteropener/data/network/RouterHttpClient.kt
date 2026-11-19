package com.alaa.legacyrouteropener.data.network

import com.alaa.legacyrouteropener.domain.model.Endpoint
import com.alaa.legacyrouteropener.domain.model.TlsInfo

internal interface RouterHttpClient {
    fun send(request: RouterRequest, cookies: String): UpstreamResult
}

internal data class UpstreamResult(val response: RouterResponse, val endpoint: Endpoint, val tlsInfo: TlsInfo)
