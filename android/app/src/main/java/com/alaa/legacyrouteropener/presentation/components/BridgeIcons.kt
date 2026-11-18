package com.alaa.legacyrouteropener.presentation.components

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.PathBuilder
import androidx.compose.ui.graphics.vector.path
import androidx.compose.ui.unit.dp

internal object BridgeIcons {
    val Globe = lineIcon("Language") {
        moveTo(21f, 12f)
        curveTo(21f, 17f, 17f, 21f, 12f, 21f)
        curveTo(7f, 21f, 3f, 17f, 3f, 12f)
        curveTo(3f, 7f, 7f, 3f, 12f, 3f)
        curveTo(17f, 3f, 21f, 7f, 21f, 12f)
        moveTo(3f, 12f)
        lineTo(21f, 12f)
        moveTo(12f, 3f)
        curveTo(7f, 8f, 7f, 16f, 12f, 21f)
        curveTo(17f, 16f, 17f, 8f, 12f, 3f)
    }

    val Back = lineIcon("Back", mirror = true) {
        moveTo(20f, 12f)
        lineTo(4f, 12f)
        moveTo(11f, 5f)
        lineTo(4f, 12f)
        lineTo(11f, 19f)
    }

    val Refresh = lineIcon("Reload") {
        moveTo(20f, 4f)
        lineTo(20f, 10f)
        lineTo(14f, 10f)
        moveTo(20f, 10f)
        curveTo(18f, 3f, 8f, 2f, 5f, 8f)
        curveTo(1f, 15f, 7f, 22f, 14f, 20f)
        curveTo(17f, 19f, 19f, 17f, 20f, 15f)
    }

    val Open = lineIcon("Open external", mirror = true) {
        moveTo(14f, 4f)
        lineTo(20f, 4f)
        lineTo(20f, 10f)
        moveTo(20f, 4f)
        lineTo(11f, 13f)
        moveTo(10f, 5f)
        lineTo(5f, 5f)
        lineTo(5f, 19f)
        lineTo(19f, 19f)
        lineTo(19f, 14f)
    }

    private fun lineIcon(name: String, mirror: Boolean = false, draw: PathBuilder.() -> Unit) =
        ImageVector.Builder(name, 24.dp, 24.dp, 24f, 24f, autoMirror = mirror)
            .path(stroke = SolidColor(Color.Black), strokeLineWidth = 1.8f, pathBuilder = draw)
            .build()
}
