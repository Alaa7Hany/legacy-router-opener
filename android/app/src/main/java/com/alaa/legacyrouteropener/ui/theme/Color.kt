package com.alaa.legacyrouteropener.ui.theme

import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color

internal data class BridgeColors(
    val background: Color,
    val card: Color,
    val alternate: Color,
    val terminal: Color,
    val border: Color,
    val strongBorder: Color,
    val text: Color,
    val muted: Color,
    val primary: Color,
    val link: Color,
    val success: Color,
    val error: Color,
    val warning: Color,
    val info: Color
)

internal val lightBridge = BridgeColors(
    Color(0xFFF4F3EF), Color.White, Color(0xFFF9F8F6), Color(0xFFFAFAF9),
    Color(0xFFD6D3CD), Color(0xFFA8A29E), Color(0xFF1C1917), Color(0xFF57534E),
    Color(0xFF1D4ED8), Color(0xFF1D4ED8), Color(0xFF15803D), Color(0xFFB91C1C),
    Color(0xFFB45309), Color(0xFF0369A1)
)

internal val darkBridge = BridgeColors(
    Color(0xFF18181B), Color(0xFF222226), Color(0xFF1C1C20), Color(0xFF141417),
    Color(0xFF3F3F46), Color(0xFF52525B), Color(0xFFF4F4F5), Color(0xFFA1A1AA),
    Color(0xFF2563EB), Color(0xFF93C5FD), Color(0xFF4ADE80), Color(0xFFF87171),
    Color(0xFFFBBF24), Color(0xFF7DD3FC)
)

internal val LocalBridgeColors = staticCompositionLocalOf { lightBridge }
