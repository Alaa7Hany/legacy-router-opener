package com.alaa.legacyrouteropener.presentation.screens

import android.content.ActivityNotFoundException
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.provider.Browser
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.layout.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.viewinterop.AndroidView
import androidx.lifecycle.lifecycleScope
import com.alaa.legacyrouteropener.BuildConfig
import com.alaa.legacyrouteropener.LegacyRouterApplication
import com.alaa.legacyrouteropener.presentation.components.BridgeIcons
import com.alaa.legacyrouteropener.ui.theme.LegacyRouterOpenerTheme
import com.alaa.legacyrouteropener.ui.theme.LocalBridgeColors
import kotlinx.coroutines.flow.first
import kotlinx.coroutines.launch
import java.io.ByteArrayInputStream

class WebViewActivity : ComponentActivity() {
    private var webView: WebView? = null

    @OptIn(ExperimentalMaterial3Api::class)
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val app = application as LegacyRouterApplication
        val url = intent.getStringExtra(URL)
        if (url == null || url != app.controller.state.value.activeUrl) {
            finish()
            return
        }
        val origin = Uri.parse(url)
        enableEdgeToEdge()
        WebView.setWebContentsDebuggingEnabled(BuildConfig.DEBUG)
        lifecycleScope.launch {
            app.controller.state.first { it.activeUrl != url }
            webView?.stopLoading()
            webView?.clearHistory()
            webView?.clearFormData()
            finish()
        }

        setContent {
            val state by app.controller.state.collectAsState()
            LegacyRouterOpenerTheme(state.language) {
                val colors = LocalBridgeColors.current
                var canGoBack by remember { mutableStateOf(false) }
                var progress by remember { mutableIntStateOf(0) }
                var noBrowser by remember { mutableStateOf(false) }
                fun msg(key: String) = app.messages[state.language, key]
                BackHandler(canGoBack) { webView?.goBack() }

                Scaffold(
                    containerColor = colors.background,
                    topBar = {
                        TopAppBar(
                            title = { Text(msg("router_page"), style = MaterialTheme.typography.titleMedium) },
                            navigationIcon = {
                                IconButton(onClick = { if (canGoBack) webView?.goBack() else finish() }) {
                                    Icon(BridgeIcons.Back, msg("back_to_opener"))
                                }
                            },
                            actions = {
                                IconButton(onClick = { webView?.reload() }) {
                                    Icon(BridgeIcons.Refresh, msg("reload"))
                                }
                                IconButton(onClick = {
                                    val current = webView?.url?.let(Uri::parse)?.takeIf { isOrigin(it, origin) } ?: origin
                                    try {
                                        startActivity(Intent(Intent.ACTION_VIEW, current)
                                            .putExtra(Browser.EXTRA_CREATE_NEW_TAB, true))
                                    } catch (_: ActivityNotFoundException) {
                                        noBrowser = true
                                    }
                                }) { Icon(BridgeIcons.Open, msg("open_external")) }
                            },
                            colors = TopAppBarDefaults.topAppBarColors(containerColor = colors.card)
                        )
                    }
                ) { insets ->
                    Column(Modifier.fillMaxSize().padding(insets).consumeWindowInsets(insets)) {
                        if (progress < 100) {
                            LinearProgressIndicator(progress = { progress / 100f }, modifier = Modifier.fillMaxWidth())
                        }
                        if (noBrowser) Text(msg("browser_unavailable"), color = colors.error)
                        val background = colors.card.toArgb()
                        AndroidView(
                            modifier = Modifier.fillMaxWidth().weight(1f),
                            factory = { context ->
                                WebView(context).apply {
                                    setBackgroundColor(background)
                                    @Suppress("SetJavaScriptEnabled")
                                    settings.javaScriptEnabled = true
                                    settings.domStorageEnabled = true
                                    settings.allowFileAccess = false
                                    settings.allowContentAccess = false
                                    settings.mixedContentMode = android.webkit.WebSettings.MIXED_CONTENT_NEVER_ALLOW
                                    settings.setSupportMultipleWindows(false)
                                    android.webkit.CookieManager.getInstance().setAcceptThirdPartyCookies(this, false)
                                    webChromeClient = object : WebChromeClient() {
                                        override fun onProgressChanged(view: WebView, newProgress: Int) {
                                            progress = newProgress
                                        }
                                    }
                                    webViewClient = object : WebViewClient() {
                                        override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest) =
                                            !isOrigin(request.url, origin)

                                        override fun shouldInterceptRequest(view: WebView, request: WebResourceRequest): WebResourceResponse? {
                                            if (isOrigin(request.url, origin) || request.url.scheme in setOf("data", "blob")) return null
                                            return WebResourceResponse("text/plain", "utf-8", 403, "Forbidden", emptyMap(),
                                                ByteArrayInputStream(ByteArray(0)))
                                        }

                                        override fun doUpdateVisitedHistory(view: WebView, url: String?, isReload: Boolean) {
                                            canGoBack = view.canGoBack()
                                        }
                                    }
                                    webView = this
                                    loadUrl(url)
                                }
                            },
                            update = { it.setBackgroundColor(background) },
                            onRelease = { view ->
                                view.stopLoading()
                                view.destroy()
                                if (webView === view) webView = null
                            }
                        )
                    }
                }
            }
        }
    }

    private fun isOrigin(uri: Uri, origin: Uri) =
        uri.scheme == origin.scheme && uri.host == origin.host && uri.port == origin.port

    companion object {
        const val URL = "URL"
    }
}
