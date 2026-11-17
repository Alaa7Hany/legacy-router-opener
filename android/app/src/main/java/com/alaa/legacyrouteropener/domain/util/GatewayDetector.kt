package com.alaa.legacyrouteropener.domain.util

import android.content.Context
import android.net.wifi.WifiManager
import android.text.format.Formatter

object GatewayDetector {

    @Suppress("DEPRECATION")
    fun detectGatewayIp(context: Context): String {
        return try {
            val wifiManager = context.applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
            val dhcpInfo = wifiManager?.dhcpInfo
            if (dhcpInfo != null && dhcpInfo.gateway != 0) {
                Formatter.formatIpAddress(dhcpInfo.gateway)
            } else {
                "192.168.1.1"
            }
        } catch (e: Exception) {
            "192.168.1.1"
        }
    }

    @Suppress("DEPRECATION")
    fun getConnectedWifiSsid(context: Context): String? {
        return try {
            val wifiManager = context.applicationContext.getSystemService(Context.WIFI_SERVICE) as? WifiManager
            val info = wifiManager?.connectionInfo
            val ssid = info?.ssid?.replace("\"", "")
            if (ssid != null && ssid != "<unknown ssid>") ssid else null
        } catch (e: Exception) {
            null
        }
    }
}
