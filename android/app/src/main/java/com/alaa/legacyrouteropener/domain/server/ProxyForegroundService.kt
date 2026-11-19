package com.alaa.legacyrouteropener.domain.server

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import com.alaa.legacyrouteropener.LegacyRouterApplication
import com.alaa.legacyrouteropener.R
import com.alaa.legacyrouteropener.presentation.MainActivity

class ProxyForegroundService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val app = application as LegacyRouterApplication
        if (intent?.action == ACTION_STOP) {
            app.controller.stop()
            stopForegroundCompat()
            stopSelf()
            return START_NOT_STICKY
        }

        val state = app.controller.state.value
        val target = intent?.getStringExtra(EXTRA_TARGET) ?: state.activeTarget
        val notification = buildNotification(app, state.language, target)
        val type = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE
        } else {
            0
        }
        ServiceCompat.startForeground(this, NOTIFICATION_ID, notification, type)

        if (!state.isRunning) {
            stopForegroundCompat()
            stopSelf()
            return START_NOT_STICKY
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

    private fun buildNotification(app: LegacyRouterApplication, language: String, target: String?): Notification {
        ensureChannel(app, language)
        val title = app.messages[language, "app_title"]
        val activeText = app.messages[language, "proxy_active"]
        val text = if (!target.isNullOrBlank()) "$activeText ($target)" else activeText
        val openIntent = PendingIntent.getActivity(
            this, 0,
            Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP),
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
            .addAction(0, app.messages[language, "stop_proxy"], stopIntent)
            .build()
    }

    private fun ensureChannel(app: LegacyRouterApplication, language: String) {
        val manager = getSystemService(NotificationManager::class.java) ?: return
        val channel = NotificationChannel(
            CHANNEL_ID,
            app.messages[language, "app_title"],
            NotificationManager.IMPORTANCE_LOW
        ).apply {
            setShowBadge(false)
            description = app.messages[language, "proxy_active"]
        }
        manager.createNotificationChannel(channel)
    }

    companion object {
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
        }

        fun stop(context: Context) {
            runCatching { context.stopService(Intent(context, ProxyForegroundService::class.java)) }
        }
    }
}
