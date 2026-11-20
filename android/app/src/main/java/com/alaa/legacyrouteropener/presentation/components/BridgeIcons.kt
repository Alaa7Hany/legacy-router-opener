package com.alaa.legacyrouteropener.presentation.components

import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.graphics.vector.PathBuilder
import androidx.compose.ui.graphics.vector.addPathNodes
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

    val GitHub = fillIcon(
        "GitHub",
        "M12 .5C5.65.5.5 5.65.5 12c0 5.08 3.29 9.39 7.86 10.91.58.11.79-.25.79-.56 0-.27-.01-1.17-.02-2.12-3.2.69-3.88-1.36-3.88-1.36-.52-1.33-1.28-1.68-1.28-1.68-1.05-.71.08-.7.08-.7 1.16.08 1.77 1.19 1.77 1.19 1.03 1.76 2.7 1.25 3.36.96.1-.75.4-1.25.73-1.54-2.55-.29-5.24-1.28-5.24-5.68 0-1.26.45-2.28 1.19-3.09-.12-.29-.52-1.46.11-3.05 0 0 .97-.31 3.16 1.18a10.9 10.9 0 0 1 5.76 0c2.19-1.49 3.16-1.18 3.16-1.18.63 1.59.23 2.76.11 3.05.74.81 1.19 1.83 1.19 3.09 0 4.41-2.69 5.39-5.26 5.67.42.36.79 1.07.79 2.16 0 1.56-.01 2.82-.01 3.2 0 .31.21.67.8.56A11.51 11.51 0 0 0 23.5 12C23.5 5.65 18.35.5 12 .5Z"
    )

    private fun lineIcon(name: String, mirror: Boolean = false, draw: PathBuilder.() -> Unit) =
        ImageVector.Builder(name, 24.dp, 24.dp, 24f, 24f, autoMirror = mirror)
            .path(stroke = SolidColor(Color.Black), strokeLineWidth = 1.8f, pathBuilder = draw)
            .build()

    private fun fillIcon(name: String, pathData: String) =
        ImageVector.Builder(name, 24.dp, 24.dp, 24f, 24f)
            .addPath(pathData = addPathNodes(pathData), fill = SolidColor(Color.Black))
            .build()
}
