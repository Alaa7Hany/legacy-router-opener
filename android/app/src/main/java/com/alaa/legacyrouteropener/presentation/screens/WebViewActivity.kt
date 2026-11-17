package com.alaa.legacyrouteropener.presentation.screens

import android.annotation.SuppressLint
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.webkit.*
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.layout.*
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import com.alaa.legacyrouteropener.ui.theme.DarkBg
import com.alaa.legacyrouteropener.ui.theme.LegacyRouterOpenerTheme

class WebViewActivity : ComponentActivity() {

    private var webView: WebView? = null

    @OptIn(ExperimentalMaterial3Api::class)
    @SuppressLint("SetJavaScriptEnabled")
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        val targetUrl = intent.getStringExtra("URL") ?: "http://127.0.0.1:8080"

        setContent {
            LegacyRouterOpenerTheme {
                var progress by remember { mutableIntStateOf(0) }
                var pageTitle by remember { mutableStateOf("Router Dashboard") }

                Scaffold(
                    topBar = {
                        TopAppBar(
                            title = {
                                Text(
                                    text = pageTitle,
                                    fontWeight = FontWeight.Bold,
                                    fontSize = 18.sp
                                )
                            },
                            navigationIcon = {
                                IconButton(onClick = { finish() }) {
                                    Icon(Icons.Default.ArrowBack, contentDescription = "Back", tint = Color.White)
                                }
                            },
                            actions = {
                                IconButton(onClick = { webView?.reload() }) {
                                    Icon(Icons.Default.Refresh, contentDescription = "Reload", tint = Color.White)
                                }
                                IconButton(onClick = {
                                    val browserIntent = Intent(Intent.ACTION_VIEW, Uri.parse(targetUrl))
                                    startActivity(browserIntent)
                                }) {
                                    Icon(Icons.Default.OpenInBrowser, contentDescription = "Open in Chrome", tint = Color.White)
                                }
                            },
                            colors = TopAppBarDefaults.topAppBarColors(
                                containerColor = DarkBg,
                                titleContentColor = Color.White
                            )
                        )
                    }
                ) { padding ->
                    Column(
                        modifier = Modifier
                            .fillMaxSize()
                            .padding(padding)
                    ) {
                        if (progress in 1..99) {
                            LinearProgressIndicator(
                                progress = { progress / 100f },
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .height(3.dp),
                                color = MaterialTheme.colorScheme.primary
                            )
                        }

                        AndroidView(
                            factory = { ctx ->
                                WebView(ctx).apply {
                                    webView = this
                                    setBackgroundColor(android.graphics.Color.WHITE)
                                    WebView.setWebContentsDebuggingEnabled(true)

                                    val cookieManager = CookieManager.getInstance()
                                    cookieManager.setAcceptCookie(true)
                                    cookieManager.setAcceptThirdPartyCookies(this, true)

                                    settings.javaScriptEnabled = true
                                    settings.domStorageEnabled = true
                                    settings.databaseEnabled = true
                                    settings.allowContentAccess = true
                                    settings.allowFileAccess = true
                                    settings.allowFileAccessFromFileURLs = true
                                    settings.allowUniversalAccessFromFileURLs = true
                                    settings.loadWithOverviewMode = true
                                    settings.useWideViewPort = true
                                    settings.setSupportZoom(true)
                                    settings.builtInZoomControls = true
                                    settings.displayZoomControls = false
                                    settings.javaScriptCanOpenWindowsAutomatically = true
                                    settings.setSupportMultipleWindows(false)
                                    settings.mixedContentMode = WebSettings.MIXED_CONTENT_ALWAYS_ALLOW
                                    settings.layoutAlgorithm = WebSettings.LayoutAlgorithm.NORMAL
                                    settings.defaultTextEncodingName = "utf-8"
                                    settings.userAgentString = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

                                    webChromeClient = object : WebChromeClient() {
                                        override fun onProgressChanged(view: WebView?, newProgress: Int) {
                                            progress = newProgress
                                        }

                                        override fun onReceivedTitle(view: WebView?, title: String?) {
                                            if (!title.isNullOrBlank()) {
                                                pageTitle = title
                                            }
                                        }

                                        override fun onConsoleMessage(consoleMessage: ConsoleMessage?): Boolean {
                                            val msg = "[${consoleMessage?.messageLevel()}] ${consoleMessage?.message()} (${consoleMessage?.sourceId()}:${consoleMessage?.lineNumber()})"
                                            android.util.Log.d("WebViewConsole", msg)
                                            android.util.Log.d("DirectRouterHttp", "🌐 [JS Console] $msg")
                                            return super.onConsoleMessage(consoleMessage)
                                        }

                                        override fun onJsAlert(view: WebView?, url: String?, message: String?, result: JsResult?): Boolean {
                                            android.util.Log.d("WebViewAlert", "JS Alert: $message")
                                            android.util.Log.d("DirectRouterHttp", "🚨 JS Alert: $message")
                                            return super.onJsAlert(view, url, message, result)
                                        }

                                        override fun onJsConfirm(view: WebView?, url: String?, message: String?, result: JsResult?): Boolean {
                                            android.util.Log.d("WebViewConfirm", "JS Confirm: $message")
                                            return super.onJsConfirm(view, url, message, result)
                                        }
                                    }

                                    webViewClient = object : WebViewClient() {
                                        override fun shouldOverrideUrlLoading(view: WebView?, request: WebResourceRequest?): Boolean {
                                            val url = request?.url?.toString()
                                            android.util.Log.d("WebViewClient", "shouldOverrideUrlLoading: $url")
                                            android.util.Log.d("DirectRouterHttp", "🔄 WebView Navigating: $url")
                                            return false
                                        }

                                        override fun onPageStarted(view: WebView?, url: String?, favicon: android.graphics.Bitmap?) {
                                            super.onPageStarted(view, url, favicon)
                                            android.util.Log.d("WebViewClient", "onPageStarted: $url")
                                            android.util.Log.d("DirectRouterHttp", "🏁 Page Started: $url")
                                        }

                                        override fun onPageFinished(view: WebView?, url: String?) {
                                            super.onPageFinished(view, url)
                                            android.util.Log.d("WebViewClient", "onPageFinished: $url")
                                            android.util.Log.d("DirectRouterHttp", "✅ Page Finished: $url")
                                        }

                                        override fun onReceivedError(
                                            view: WebView?,
                                            request: WebResourceRequest?,
                                            error: WebResourceError?
                                        ) {
                                            super.onReceivedError(view, request, error)
                                            val errStr = "onReceivedError: ${request?.url} -> ${error?.description} (${error?.errorCode})"
                                            android.util.Log.e("WebViewClient", errStr)
                                            android.util.Log.e("DirectRouterHttp", "❌ WebView Error: $errStr")
                                        }

                                        override fun onReceivedHttpError(
                                            view: WebView?,
                                            request: WebResourceRequest?,
                                            errorResponse: WebResourceResponse?
                                        ) {
                                            super.onReceivedHttpError(view, request, errorResponse)
                                            val errStr = "onReceivedHttpError: ${request?.url} -> ${errorResponse?.statusCode} ${errorResponse?.reasonPhrase}"
                                            android.util.Log.e("WebViewClient", errStr)
                                            android.util.Log.e("DirectRouterHttp", "❌ WebView HTTP Error: $errStr")
                                        }
                                    }

                                    loadUrl(targetUrl)
                                }
                            },
                            modifier = Modifier.fillMaxSize()
                        )
                    }
                }
            }
        }
    }

    override fun onBackPressed() {
        if (webView?.canGoBack() == true) {
            webView?.goBack()
        } else {
            super.onBackPressed()
        }
    }

    override fun onDestroy() {
        webView?.destroy()
        super.onDestroy()
    }
}
