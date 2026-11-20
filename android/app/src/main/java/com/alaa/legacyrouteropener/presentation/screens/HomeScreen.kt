package com.alaa.legacyrouteropener.presentation.screens

import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.text.style.TextDirection
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.alaa.legacyrouteropener.domain.model.AppLanguage
import com.alaa.legacyrouteropener.domain.model.BannerMessage
import com.alaa.legacyrouteropener.domain.model.HardwarePreset
import com.alaa.legacyrouteropener.domain.model.ProxyState
import com.alaa.legacyrouteropener.domain.model.TlsInfo
import com.alaa.legacyrouteropener.presentation.components.BridgeFooter
import com.alaa.legacyrouteropener.presentation.components.BridgeIcons
import com.alaa.legacyrouteropener.ui.theme.LocalBridgeColors
import kotlinx.coroutines.flow.StateFlow

@Immutable
internal data class HomeActions(
    val onOpenInApp: () -> Unit,
    val onOpenExternal: () -> Unit,
    val onUpdateRouterIp: (String) -> Unit,
    val onChoosePreset: (HardwarePreset) -> Unit,
    val onChooseLanguage: (AppLanguage) -> Unit,
    val onUseDetected: () -> Unit,
    val onApplyChanges: () -> Unit,
    val onResetLogin: () -> Unit,
    val onStopProxy: () -> Unit,
    val onClearLogs: () -> Unit,
    val onOpenUrl: (String) -> Unit
)

@Composable
internal fun HomeScreen(
    state: ProxyState,
    logsFlow: StateFlow<List<String>>,
    opening: Boolean,
    browserUnavailable: Boolean,
    msg: (String) -> String,
    actions: HomeActions
) {
    val colors = LocalBridgeColors.current
    val addressFocus = remember { FocusRequester() }
    var deviceExpanded by rememberSaveable { mutableStateOf(false) }
    var detailsExpanded by rememberSaveable { mutableStateOf(false) }
    var loginHelp by rememberSaveable { mutableStateOf(false) }
    var addressHelp by rememberSaveable { mutableStateOf(false) }
    val rtl = LocalLayoutDirection.current == LayoutDirection.Rtl

    LaunchedEffect(state.validationError) {
        if (state.validationError != null) {
            deviceExpanded = true
            withFrameNanos { }
            addressFocus.requestFocus()
        }
    }

    Scaffold(
        containerColor = colors.background,
        contentWindowInsets = WindowInsets.systemBars.union(WindowInsets.displayCutout)
    ) { insets ->
        BoxWithConstraints(Modifier.fillMaxSize().padding(insets).consumeWindowInsets(insets).imePadding()) {
            val viewport = maxHeight
            Column(
                modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState())
                    .heightIn(min = viewport).padding(horizontal = 16.dp, vertical = 24.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.Center
            ) {
                Surface(
                    modifier = Modifier.widthIn(max = 420.dp).fillMaxWidth(),
                    shape = RoundedCornerShape(12.dp), color = colors.card,
                    border = BorderStroke(1.dp, colors.border), shadowElevation = 1.dp
                ) {
                    Column(Modifier.padding(24.dp)) {
                        OpenerHeader(state.language, msg, actions.onChooseLanguage)
                        Text(msg("device_address") + " " + isolate(state.activeTarget ?: state.routerIp),
                            color = colors.muted, fontSize = 14.sp)
                        Spacer(Modifier.height(20.dp))
                        Button(
                            onClick = actions.onOpenInApp, enabled = !opening,
                            modifier = Modifier.fillMaxWidth().heightIn(min = 50.dp),
                            shape = RoundedCornerShape(8.dp)
                        ) {
                            Text(msg("open_router"), fontSize = 16.sp, fontWeight = FontWeight.SemiBold)
                            Spacer(Modifier.width(8.dp))
                            Text(if (rtl) "↖" else "↗", fontSize = 17.sp)
                        }
                        Row(Modifier.align(Alignment.CenterHorizontally)) {
                            BridgeLink(msg("open_external"), enabled = !opening, onClick = actions.onOpenExternal)
                        }
                        Row(Modifier.align(Alignment.CenterHorizontally), verticalAlignment = Alignment.CenterVertically) {
                            BridgeLink(msg("fix_login"), onClick = actions.onResetLogin)
                            HelpButton(msg("login_help_label")) { loginHelp = !loginHelp }
                        }
                        if (loginHelp) InlineNote(msg("login_help"))
                        val banner = if (browserUnavailable) BannerMessage.BROWSER_UNAVAILABLE else state.banner
                        if (banner != null) {
                            InlineNote(msg(banner.messageKey), if (banner.isError) colors.error else colors.success)
                        }
                        HorizontalDivider(color = colors.border)
                        Disclosure(msg("change_device"), deviceExpanded, msg) { deviceExpanded = !deviceExpanded }
                        if (deviceExpanded) {
                            DeviceSettingsSection(
                                state = state,
                                addressFocus = addressFocus,
                                addressHelp = addressHelp,
                                onToggleAddressHelp = { addressHelp = !addressHelp },
                                msg = msg,
                                actions = actions
                            )
                        }
                        HorizontalDivider(color = colors.border)
                        Disclosure(msg("connection_details"), detailsExpanded, msg) { detailsExpanded = !detailsExpanded }
                        if (detailsExpanded) {
                            ConnectionDetails(state.tlsInfo, logsFlow, msg, actions.onClearLogs)
                        }
                        if (state.isRunning) {
                            HorizontalDivider(color = colors.border)
                            Column(Modifier.fillMaxWidth().padding(top = 12.dp),
                                horizontalAlignment = Alignment.CenterHorizontally) {
                                Text(msg("proxy_active"), color = colors.muted, fontSize = 13.sp)
                                BridgeLink(msg("stop_proxy"), onClick = actions.onStopProxy)
                            }
                        }
                    }
                }
                Spacer(Modifier.height(8.dp))
                BridgeFooter(msg = msg, onOpenUrl = actions.onOpenUrl)
            }
        }
    }
}

@Composable
private fun OpenerHeader(
    selectedLanguage: AppLanguage,
    msg: (String) -> String,
    onChooseLanguage: (AppLanguage) -> Unit
) {
    val colors = LocalBridgeColors.current
    var languageMenu by remember { mutableStateOf(false) }
    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
        Text(msg("app_title"), modifier = Modifier.weight(1f), fontSize = 20.sp,
            fontWeight = FontWeight.Bold, lineHeight = 25.sp)
        Box {
            TextButton(onClick = { languageMenu = true },
                contentPadding = PaddingValues(horizontal = 4.dp)) {
                Icon(BridgeIcons.Globe, null, Modifier.size(17.dp), tint = colors.muted)
                Spacer(Modifier.width(4.dp))
                Text(msg("language_label"), fontSize = 13.sp, color = colors.muted)
            }
            DropdownMenu(expanded = languageMenu, onDismissRequest = { languageMenu = false }) {
                for (language in AppLanguage.entries) {
                    DropdownMenuItem(
                        text = {
                            Text(
                                language.displayName,
                                fontWeight = if (selectedLanguage == language) FontWeight.Bold else FontWeight.Normal
                            )
                        },
                        onClick = {
                            onChooseLanguage(language)
                            languageMenu = false
                        }
                    )
                }
            }
        }
    }
}

@Composable
private fun DeviceSettingsSection(
    state: ProxyState,
    addressFocus: FocusRequester,
    addressHelp: Boolean,
    onToggleAddressHelp: () -> Unit,
    msg: (String) -> String,
    actions: HomeActions
) {
    val colors = LocalBridgeColors.current
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(msg("device_address"), fontWeight = FontWeight.SemiBold, fontSize = 14.sp)
        HelpButton(msg("address_help_label"), onToggleAddressHelp)
    }
    if (addressHelp) {
        InlineNote(msg("address_help") + " " + msg("example") + " " + isolate("192.168.1.1:443"))
    }
    OutlinedTextField(
        value = state.routerIp, onValueChange = actions.onUpdateRouterIp,
        modifier = Modifier.fillMaxWidth().focusRequester(addressFocus)
            .semantics { contentDescription = msg("device_address") },
        singleLine = true, isError = state.validationError != null,
        textStyle = MaterialTheme.typography.bodyMedium.copy(textDirection = TextDirection.Ltr),
        placeholder = { Text("192.168.1.1") },
        supportingText = state.validationError?.let { error -> { Text(msg(error.messageKey)) } }
    )
    state.detectedGateway?.let { gateway ->
        BridgeLink(msg("use_detected") + " " + isolate(gateway), onClick = actions.onUseDetected)
    }
    Spacer(Modifier.height(8.dp))
    PresetPicker(state.presetId, msg, actions.onChoosePreset)
    Spacer(Modifier.height(12.dp))
    OutlinedButton(
        onClick = actions.onApplyChanges,
        border = BorderStroke(1.dp, colors.strongBorder), shape = RoundedCornerShape(8.dp),
        colors = ButtonDefaults.outlinedButtonColors(containerColor = colors.alternate,
            contentColor = colors.text)
    ) { Text(msg("apply")) }
    Spacer(Modifier.height(8.dp))
}

@Composable
private fun PresetPicker(id: String, msg: (String) -> String, choose: (HardwarePreset) -> Unit) {
    val colors = LocalBridgeColors.current
    var expanded by remember { mutableStateOf(false) }
    Text(msg("device_type"), fontWeight = FontWeight.SemiBold, fontSize = 14.sp)
    Box {
        OutlinedButton(
            onClick = { expanded = true }, modifier = Modifier.fillMaxWidth(),
            shape = RoundedCornerShape(8.dp), border = BorderStroke(1.dp, colors.strongBorder),
            colors = ButtonDefaults.outlinedButtonColors(contentColor = colors.text)
        ) {
            Text(msg("preset_${id}_name"), modifier = Modifier.weight(1f))
            Text("▾", color = colors.muted)
        }
        DropdownMenu(expanded, onDismissRequest = { expanded = false }) {
            HardwarePreset.all.forEach { preset ->
                DropdownMenuItem(text = { Text(msg("preset_${preset.id}_name")) }, onClick = {
                    choose(preset)
                    expanded = false
                })
            }
        }
    }
    Text(msg("preset_${id}_help"), color = colors.muted, fontSize = 13.sp)
}

@Composable
private fun ConnectionDetails(
    tlsInfo: TlsInfo?,
    logsFlow: StateFlow<List<String>>,
    msg: (String) -> String,
    onClear: () -> Unit
) {
    val colors = LocalBridgeColors.current
    val logEntries by logsFlow.collectAsStateWithLifecycle()
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(
            tlsInfo?.let { isolate(listOfNotNull(it.protocol, it.cipherSuite).joinToString(" · ")) }
                ?: msg("connection_idle"),
            modifier = Modifier.weight(1f), color = colors.muted, fontSize = 13.sp
        )
        BridgeLink(msg("clear"), onClick = onClear)
    }
    tlsInfo?.takeIf { it.peerSubject != null }?.let {
        Text(msg("identity_unverified") + " · " + isolate(it.peerSubject.orEmpty()),
            color = colors.warning, fontSize = 13.sp)
    }
    val logs = rememberLazyListState()
    LaunchedEffect(logEntries) {
        if (logEntries.isNotEmpty()) logs.scrollToItem(logEntries.lastIndex)
    }
    Surface(
        color = colors.terminal, border = BorderStroke(1.dp, colors.border), shape = RoundedCornerShape(8.dp),
        modifier = Modifier.fillMaxWidth().padding(vertical = 8.dp).height(185.dp)
    ) {
        CompositionLocalProvider(LocalLayoutDirection provides LayoutDirection.Ltr) {
            LazyColumn(state = logs, contentPadding = PaddingValues(12.dp),
                verticalArrangement = Arrangement.spacedBy(2.dp)) {
                if (logEntries.isEmpty()) item { Text(msg("logs_empty"), color = colors.muted, fontSize = 13.sp) }
                items(logEntries) { line ->
                    val color = when {
                        line.startsWith("[ERR]") -> colors.error
                        line.startsWith("[CACHE]") -> colors.info
                        line.startsWith("[2") -> colors.success
                        else -> colors.text
                    }
                    Text(line, fontSize = 12.sp, lineHeight = 17.sp, fontFamily = FontFamily.Monospace, color = color)
                }
            }
        }
    }
}

@Composable
private fun Disclosure(title: String, open: Boolean, msg: (String) -> String, toggle: () -> Unit) {
    Row(Modifier.fillMaxWidth().heightIn(min = 48.dp)
        .clickable(role = Role.Button, onClick = toggle)
        .semantics { stateDescription = msg(if (open) "expanded" else "collapsed") },
        verticalAlignment = Alignment.CenterVertically) {
        Text(title, fontWeight = FontWeight.SemiBold, modifier = Modifier.weight(1f), fontSize = 14.sp)
        Text(if (open) "−" else "+", fontSize = 18.sp, color = LocalBridgeColors.current.muted)
    }
}

@Composable
private fun HelpButton(description: String, action: () -> Unit) {
    val colors = LocalBridgeColors.current
    IconButton(onClick = action, modifier = Modifier.semantics { contentDescription = description }) {
        Surface(color = colors.alternate, shape = RoundedCornerShape(14.dp), border = BorderStroke(1.dp, colors.strongBorder)) {
            Box(Modifier.size(25.dp), contentAlignment = Alignment.Center) {
                Text("?", fontSize = 13.sp, fontWeight = FontWeight.SemiBold)
            }
        }
    }
}

@Composable
private fun BridgeLink(label: String, enabled: Boolean = true, onClick: () -> Unit) {
    TextButton(onClick = onClick, enabled = enabled, contentPadding = PaddingValues(horizontal = 4.dp)) {
        Text(label, textDecoration = TextDecoration.Underline, fontSize = 13.5.sp,
            fontWeight = FontWeight.SemiBold, color = LocalBridgeColors.current.link)
    }
}

@Composable
private fun InlineNote(text: String, accent: androidx.compose.ui.graphics.Color = LocalBridgeColors.current.strongBorder) {
    Surface(color = LocalBridgeColors.current.alternate, shape = RoundedCornerShape(6.dp),
        border = BorderStroke(1.dp, accent),
        modifier = Modifier.fillMaxWidth().padding(bottom = 12.dp)) {
        Text(text, modifier = Modifier.padding(10.dp), fontSize = 13.sp, color = LocalBridgeColors.current.text)
    }
}

private fun isolate(text: String) = "\u2066$text\u2069"
