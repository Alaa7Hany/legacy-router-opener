package com.alaa.legacyrouteropener.ui.theme

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.unit.LayoutDirection

@Composable
fun LegacyRouterOpenerTheme(language: String, content: @Composable () -> Unit) {
    val dark = isSystemInDarkTheme()
    val colors = if (dark) darkBridge else lightBridge
    val base = if (dark) darkColorScheme() else lightColorScheme()
    val scheme = base.copy(
        primary = colors.primary, onPrimary = Color.White,
        background = colors.background, onBackground = colors.text,
        surface = colors.card, onSurface = colors.text,
        surfaceVariant = colors.alternate, onSurfaceVariant = colors.muted,
        outline = colors.strongBorder, outlineVariant = colors.border,
        error = colors.error
    )
    CompositionLocalProvider(
        LocalBridgeColors provides colors,
        LocalLayoutDirection provides if (language == "ar") LayoutDirection.Rtl else LayoutDirection.Ltr
    ) {
        MaterialTheme(colorScheme = scheme, typography = Typography, content = content)
    }
}
