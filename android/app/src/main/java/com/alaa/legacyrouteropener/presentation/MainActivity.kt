package com.alaa.legacyrouteropener.presentation

import android.Manifest
import android.content.ActivityNotFoundException
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.Browser
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.lifecycleScope
import com.alaa.legacyrouteropener.LegacyRouterApplication
import com.alaa.legacyrouteropener.presentation.screens.HomeActions
import com.alaa.legacyrouteropener.presentation.screens.HomeScreen
import com.alaa.legacyrouteropener.presentation.screens.WebViewActivity
import com.alaa.legacyrouteropener.ui.theme.LegacyRouterOpenerTheme
import kotlinx.coroutines.launch

class MainActivity : ComponentActivity() {
    private val container get() = (application as LegacyRouterApplication).container
    private val viewModel: MainViewModel by viewModels { container.mainViewModelFactory }

    private var opening by mutableStateOf(false)
    private var browserUnavailable by mutableStateOf(false)
    private var notificationPrompted = false
    private var pendingExternalLaunch: (() -> Unit)? = null

    private val notificationPermissionLauncher =
        registerForActivityResult(ActivityResultContracts.RequestPermission()) {
            val action = pendingExternalLaunch
            pendingExternalLaunch = null
            action?.invoke()
        }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        val catalog = container.messages
        val actions = HomeActions(
            onOpenInApp = { openRouter(external = false) },
            onOpenExternal = { openRouter(external = true) },
            onUpdateRouterIp = viewModel::updateRouterIp,
            onChoosePreset = viewModel::choosePreset,
            onChooseLanguage = viewModel::chooseLanguage,
            onUseDetected = viewModel::useDetected,
            onApplyChanges = viewModel::applyChanges,
            onResetLogin = viewModel::resetLogin,
            onStopProxy = viewModel::stopProxy,
            onClearLogs = viewModel::clearLogs,
            onOpenUrl = ::openExternalUrl
        )
        setContent {
            val state by viewModel.state.collectAsStateWithLifecycle()
            LegacyRouterOpenerTheme(state.language) {
                HomeScreen(
                    state = state,
                    logsFlow = viewModel.logs,
                    opening = opening,
                    browserUnavailable = browserUnavailable,
                    msg = { key -> catalog[state.language, key] },
                    actions = actions
                )
            }
        }
    }

    override fun onResume() {
        super.onResume()
        viewModel.refreshGateway()
    }

    private fun openRouter(external: Boolean) {
        if (opening) return
        if (external && shouldRequestNotifications()) {
            notificationPrompted = true
            pendingExternalLaunch = { launchRouterSession(external = true) }
            notificationPermissionLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
            return
        }
        launchRouterSession(external)
    }

    private fun shouldRequestNotifications(): Boolean {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU || notificationPrompted) return false
        return ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) !=
            PackageManager.PERMISSION_GRANTED
    }

    private fun launchRouterSession(external: Boolean) {
        if (opening) return
        opening = true
        browserUnavailable = false
        lifecycleScope.launch {
            try {
                val url = viewModel.startProxy() ?: return@launch
                val intent = if (external) {
                    Intent(Intent.ACTION_VIEW, Uri.parse(url)).putExtra(Browser.EXTRA_CREATE_NEW_TAB, true)
                } else {
                    Intent(this@MainActivity, WebViewActivity::class.java).putExtra(WebViewActivity.URL, url)
                }
                startActivity(intent)
            } catch (_: ActivityNotFoundException) {
                browserUnavailable = true
            } finally {
                opening = false
            }
        }
    }

    private fun openExternalUrl(url: String) {
        try {
            startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
        } catch (_: ActivityNotFoundException) {
            browserUnavailable = true
        }
    }
}
