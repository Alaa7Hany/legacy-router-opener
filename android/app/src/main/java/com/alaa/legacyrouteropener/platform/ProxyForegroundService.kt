package com.alaa.legacyrouteropener.platform

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.util.Log
import android.webkit.CookieManager
import android.webkit.WebStorage
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import com.alaa.legacyrouteropener.LegacyRouterApplication
import com.alaa.legacyrouteropener.R
import com.alaa.legacyrouteropener.domain.model.AppLanguage
import com.alaa.legacyrouteropener.domain.server.ProxyServiceController
import com.alaa.legacyrouteropener.domain.server.SessionCookieCleaner
import com.alaa.legacyrouteropener.presentation.MainActivity

class ProxyForegroundService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val container = (application as LegacyRouterApplication).container
        if (intent?.action == ACTION_STOP) {
            stopForegroundCompat()
            stopSelf()
            container.backgroundExecutor.execute { container.controller.stop() }
            return START_NOT_STICKY
        }

        val state = container.controller.state.value
        val target = intent?.getStringExtra(EXTRA_TARGET) ?: state.activeTarget
        val notification = buildNotification(container.messages::get, state.language, target)
        val type = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE
        } else {
            0
        }
        ServiceCompat.startForeground(this, NOTIFICATION_ID, notification, type)

        if (!state.isRunning) {
            stopForegroundCompat()
            stopSelf()
        }
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        stopForegroundCompat()
        super.onDestroy()
    }

    private fun stopForegroundCompat() {
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
    }

    private fun buildNotification(
        message: (AppLanguage, String) -> String,
        language: AppLanguage,
        target: String?
    ): Notification {
        ensureChannel(message, language)
        val title = message(language, "app_title")
        val activeText = message(language, "proxy_active")
        val text = if (!target.isNullOrBlank()) "$activeText ($target)" else activeText
        val openIntent = PendingIntent.getActivity(
            this, 0,
            Intent(this, MainActivity::class.java)
                .addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        val stopIntent = PendingIntent.getService(
            this, 1,
            Intent(this, ProxyForegroundService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_proxy_notification)
            .setContentTitle(title)
            .setContentText(text)
            .setOngoing(true)
            .setSilent(true)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setContentIntent(openIntent)
            .addAction(0, message(language, "stop_proxy"), stopIntent)
            .build()
    }

    private fun ensureChannel(message: (AppLanguage, String) -> String, language: AppLanguage) {
        val manager = getSystemService(NotificationManager::class.java) ?: return
        val channel = NotificationChannel(
            CHANNEL_ID,
            message(language, "app_title"),
            NotificationManager.IMPORTANCE_LOW
        ).apply {
            setShowBadge(false)
            description = message(language, "proxy_active")
        }
        manager.createNotificationChannel(channel)
    }

    companion object {
        private const val TAG = "ProxyForegroundService"
        private const val CHANNEL_ID = "local_proxy"
        private const val NOTIFICATION_ID = 1001
        private const val ACTION_START = "com.alaa.legacyrouteropener.action.START_PROXY"
        private const val ACTION_STOP = "com.alaa.legacyrouteropener.action.STOP_PROXY"
        private const val EXTRA_TARGET = "target"

        fun start(context: Context, target: String) {
            val intent = Intent(context, ProxyForegroundService::class.java)
                .setAction(ACTION_START)
                .putExtra(EXTRA_TARGET, target)
            runCatching { ContextCompat.startForegroundService(context, intent) }
                .onFailure { Log.w(TAG, "Unable to start foreground service: ${it.message}") }
        }

        fun stop(context: Context) {
            runCatching { context.stopService(Intent(context, ProxyForegroundService::class.java)) }
                .onFailure { Log.w(TAG, "Unable to stop foreground service: ${it.message}") }
        }
    }
}

internal class AndroidProxyServiceController(context: Context) : ProxyServiceController {
    private val appContext = context.applicationContext

    override fun start(target: String) = ProxyForegroundService.start(appContext, target)

    override fun stop() = ProxyForegroundService.stop(appContext)
}

internal class WebViewCookieCleaner(
    private val mainHandler: Handler = Handler(Looper.getMainLooper())
) : SessionCookieCleaner {
    override fun clear(url: String, cookies: List<Pair<String, String>>) {
        mainHandler.post {
            val storage = CookieManager.getInstance()
            for ((name, path) in cookies) {
                storage.setCookie(url, "$name=; Path=$path; Max-Age=0")
            }
            storage.flush()
            WebStorage.getInstance().deleteOrigin(url.trimEnd('/'))
        }
    }
}
