package com.alaa.legacyrouteropener.domain.util

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.net.wifi.WifiManager
import java.net.Inet4Address

internal object GatewayDetector {
    @Suppress("DEPRECATION")
    fun detectGatewayIp(context: Context): String? {
        val connectivity = context.getSystemService(ConnectivityManager::class.java)
        val candidates = listOfNotNull(connectivity.activeNetwork) + connectivity.allNetworks
        for (network in candidates.distinct()) {
            val capabilities = connectivity.getNetworkCapabilities(network) ?: continue
            if (!capabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) &&
                !capabilities.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET)) continue
            val route = connectivity.getLinkProperties(network)?.routes?.firstOrNull {
                it.isDefaultRoute && it.gateway is Inet4Address && it.gateway?.isAnyLocalAddress == false
            }
            route?.gateway?.hostAddress?.let { return it }
        }
        val address = context.applicationContext.getSystemService(WifiManager::class.java)
            ?.dhcpInfo?.gateway ?: 0
        if (address == 0) return null
        return (0..3).joinToString(".") { shift -> ((address ushr (shift * 8)) and 255).toString() }
    }
}
