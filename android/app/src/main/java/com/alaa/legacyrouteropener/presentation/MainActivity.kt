package com.alaa.legacyrouteropener.presentation

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.viewModels
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import com.alaa.legacyrouteropener.presentation.screens.HomeScreen
import com.alaa.legacyrouteropener.ui.theme.LegacyRouterOpenerTheme

class MainActivity : ComponentActivity() {

    private val viewModel: MainViewModel by viewModels()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        setContent {
            LegacyRouterOpenerTheme {
                val state by viewModel.state.collectAsState()

                HomeScreen(
                    state = state,
                    onRouterIpChange = { viewModel.updateRouterIp(it) },
                    onStartProxy = { viewModel.startProxy() },
                    onStopProxy = { viewModel.stopProxy() },
                    onClearLogs = { viewModel.clearLogs() }
                )
            }
        }
    }
}
