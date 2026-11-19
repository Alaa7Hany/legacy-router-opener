package com.alaa.legacyrouteropener

import android.content.Context
import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import com.alaa.legacyrouteropener.data.i18n.BridgeMessages
import com.alaa.legacyrouteropener.domain.model.AppLanguage
import com.alaa.legacyrouteropener.data.network.AndroidGatewayProvider
import com.alaa.legacyrouteropener.data.repository.SharedPreferencesStore
import com.alaa.legacyrouteropener.data.proxy.DefaultProxySessionFactory
import com.alaa.legacyrouteropener.domain.server.ProxyController
import com.alaa.legacyrouteropener.platform.AndroidProxyServiceController
import com.alaa.legacyrouteropener.platform.WebViewCookieCleaner
import com.alaa.legacyrouteropener.presentation.MainViewModel
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors

class AppContainer(context: Context) {
    private val appContext = context.applicationContext

    val backgroundExecutor: ExecutorService = Executors.newSingleThreadExecutor { runnable ->
        Thread(runnable, "router-app-bg").apply { isDaemon = true }
    }

    val messages: BridgeMessages = BridgeMessages(appContext)

    val controller: ProxyController = ProxyController(
        preferences = SharedPreferencesStore(appContext),
        gatewayProvider = AndroidGatewayProvider(appContext),
        serviceController = AndroidProxyServiceController(appContext),
        cookieCleaner = WebViewCookieCleaner(),
        sessionFactory = DefaultProxySessionFactory(),
        defaultLanguage = AppLanguage.fromLocaleLanguage(appContext.resources.configuration.locales[0].language),
        message = { language, key -> messages[language, key] }
    )

    val mainViewModelFactory: ViewModelProvider.Factory = object : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T {
            require(modelClass == MainViewModel::class.java) { "Unknown ViewModel class: ${modelClass.name}" }
            return MainViewModel(controller) as T
        }
    }
}
