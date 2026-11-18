package com.alaa.legacyrouteropener.presentation

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import com.alaa.legacyrouteropener.LegacyRouterApplication
import com.alaa.legacyrouteropener.presentation.screens.HomeScreen
import com.alaa.legacyrouteropener.ui.theme.LegacyRouterOpenerTheme

class MainActivity : ComponentActivity() {
    private val viewModel: MainViewModel by viewModels()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        val catalog = (application as LegacyRouterApplication).messages
        setContent {
            val state by viewModel.state.collectAsState()
            LegacyRouterOpenerTheme(state.language) {
                HomeScreen(state, { key -> catalog[state.language, key] }, viewModel)
            }
        }
    }
}
