package com.alaa.legacyrouteropener.data.network

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.net.wifi.WifiManager
import com.alaa.legacyrouteropener.domain.server.GatewayProvider
import java.net.Inet4Address

internal class AndroidGatewayProvider(context: Context) : GatewayProvider {
    private val appContext = context.applicationContext

    @Suppress("DEPRECATION")
    override fun detectGatewayIp(): String? {
        val connectivity = appContext.getSystemService(ConnectivityManager::class.java) ?: return null
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
        val address = appContext.getSystemService(WifiManager::class.java)?.dhcpInfo?.gateway ?: 0
        if (address == 0) return null
        return (0..3).joinToString(".") { shift -> ((address ushr (shift * 8)) and 255).toString() }
    }
}
