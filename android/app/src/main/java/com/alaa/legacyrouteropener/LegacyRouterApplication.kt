package com.alaa.legacyrouteropener

import android.app.Application
import android.os.Handler
import android.os.Looper
import android.webkit.CookieManager
import android.webkit.WebStorage
import com.alaa.legacyrouteropener.data.i18n.BridgeMessages
import com.alaa.legacyrouteropener.domain.server.ProxyController

class LegacyRouterApplication : Application() {
    lateinit var controller: ProxyController
        private set
    lateinit var messages: BridgeMessages
        private set

    override fun onCreate() {
        super.onCreate()
        messages = BridgeMessages(this)
        val main = Handler(Looper.getMainLooper())
        controller = ProxyController(this, messages) { url, cookies ->
            main.post {
                val storage = CookieManager.getInstance()
                for ((name, path) in cookies) {
                    storage.setCookie(url, "$name=; Path=$path; Max-Age=0")
                }
                WebStorage.getInstance().deleteOrigin(url.trimEnd('/'))
            }
        }
    }
}
