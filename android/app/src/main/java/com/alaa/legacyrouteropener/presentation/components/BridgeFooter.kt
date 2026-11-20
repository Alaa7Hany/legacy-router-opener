package com.alaa.legacyrouteropener.presentation.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.alaa.legacyrouteropener.ui.theme.LocalBridgeColors

private data class FooterLinkItem(
    val labelKey: String,
    val url: String,
    val icon: ImageVector
)

private val footerLinkItems = listOf(
    FooterLinkItem("footer_website", "https://3laa.me/legacy-router-opener/", BridgeIcons.Globe),
    FooterLinkItem("footer_source", "https://github.com/Alaa7Hany/legacy-router-opener", BridgeIcons.GitHub)
)

@Composable
internal fun BridgeFooter(
    msg: (String) -> String,
    onOpenUrl: (String) -> Unit,
    modifier: Modifier = Modifier
) {
    val colors = LocalBridgeColors.current
    Row(
        modifier = modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(2.dp, Alignment.CenterHorizontally),
        verticalAlignment = Alignment.CenterVertically
    ) {
        footerLinkItems.forEachIndexed { index, item ->
            if (index > 0) {
                Text("·", color = colors.muted, fontSize = 13.sp)
            }
            FooterLinkButton(
                label = msg(item.labelKey),
                icon = item.icon,
                onClick = { onOpenUrl(item.url) }
            )
        }
    }
}

@Composable
private fun FooterLinkButton(
    label: String,
    icon: ImageVector,
    onClick: () -> Unit
) {
    val muted = LocalBridgeColors.current.muted
    TextButton(
        onClick = onClick,
        contentPadding = PaddingValues(horizontal = 6.dp, vertical = 2.dp)
    ) {
        Icon(icon, contentDescription = null, modifier = Modifier.size(15.dp), tint = muted)
        Spacer(Modifier.width(5.dp))
        Text(
            label,
            textDecoration = TextDecoration.Underline,
            fontSize = 13.sp,
            color = muted
        )
    }
}
