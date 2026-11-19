package com.alaa.legacyrouteropener

import android.app.Application

class LegacyRouterApplication : Application() {
    lateinit var container: AppContainer
        private set

    override fun onCreate() {
        super.onCreate()
        container = AppContainer(this)
        container.backgroundExecutor.execute {
            container.controller.refreshGateway()
        }
    }
}
