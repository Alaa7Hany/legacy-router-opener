package com.alaa.legacyrouteropener.domain.server

import com.alaa.legacyrouteropener.domain.model.AppLanguage
import com.alaa.legacyrouteropener.domain.model.BannerMessage
import com.alaa.legacyrouteropener.domain.model.Endpoint
import com.alaa.legacyrouteropener.domain.model.EndpointParseException
import com.alaa.legacyrouteropener.domain.model.HardwarePreset
import com.alaa.legacyrouteropener.domain.model.ProxyState
import com.alaa.legacyrouteropener.domain.model.ValidationError
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update

class ProxyController(
    private val preferences: PreferencesStore,
    private val gatewayProvider: GatewayProvider,
    private val serviceController: ProxyServiceController,
    private val cookieCleaner: SessionCookieCleaner,
    private val sessionFactory: ProxySessionFactory,
    defaultLanguage: AppLanguage,
    private val message: (AppLanguage, String) -> String
) {
    private val initialSavedTarget = preferences.loadTarget()
    @Volatile private var userEditedAddress = initialSavedTarget != null

    private val mutableState = MutableStateFlow(
        ProxyState(
            routerIp = initialSavedTarget ?: "192.168.1.1",
            presetId = HardwarePreset.find(preferences.loadPresetId()).id,
            language = preferences.loadLanguage() ?: defaultLanguage
        )
    )
    val state = mutableState.asStateFlow()

    private val mutableLogs = MutableStateFlow<List<String>>(emptyList())
    val logs = mutableLogs.asStateFlow()

    private var active: ProxySession? = null

    fun refreshGateway() {
        val detected = gatewayProvider.detectGatewayIp()
        mutableState.update { current ->
            val updatedRouterIp = if (!userEditedAddress && detected != null) detected else current.routerIp
            current.copy(detectedGateway = detected, routerIp = updatedRouterIp)
        }
    }

    fun changeAddress(address: String) {
        userEditedAddress = true
        mutableState.update {
            it.copy(routerIp = address.take(1024), validationError = null, banner = null)
        }
    }

    fun choosePreset(preset: HardwarePreset) = mutableState.update {
        it.copy(presetId = preset.id, validationError = null, banner = null)
    }

    fun useDetected() {
        val detected = mutableState.value.detectedGateway
        if (detected == null) {
            mutableState.update { it.copy(banner = BannerMessage.DETECTION_FAILED) }
        } else {
            userEditedAddress = true
            mutableState.update {
                it.copy(routerIp = detected, validationError = null, banner = BannerMessage.DETECTED_SAVED)
            }
        }
    }

    fun chooseLanguage(language: AppLanguage) {
        mutableState.update { it.copy(language = language) }
        preferences.saveLanguage(language)
        synchronized(this) { active?.endpoint?.authority }?.let(serviceController::start)
    }

    fun clearLogs() {
        mutableLogs.value = emptyList()
    }

    @Synchronized
    fun open(): String? = applyTarget(start = true, announce = false)

    @Synchronized
    fun applyChanges() {
        applyTarget(start = active != null, announce = true)
    }

    @Synchronized
    fun resetLogin() {
        val current = active
        if (current == null) {
            mutableState.update { it.copy(banner = BannerMessage.LOGIN_FIXED, validationError = null) }
            return
        }
        if (replace(current.endpoint, current.preset) == null) {
            mutableState.update { it.copy(banner = BannerMessage.LOGIN_FIX_FAILED) }
        } else {
            mutableState.update { it.copy(banner = BannerMessage.LOGIN_FIXED, validationError = null) }
        }
    }

    private fun applyTarget(start: Boolean, announce: Boolean): String? {
        val draft = mutableState.value
        val endpoint = try {
            Endpoint.parse(draft.routerIp)
        } catch (failure: EndpointParseException) {
            mutableState.update { it.copy(validationError = failure.error) }
            return null
        } catch (_: IllegalArgumentException) {
            mutableState.update { it.copy(validationError = ValidationError.ADDRESS_INVALID) }
            return null
        }
        val preset = HardwarePreset.find(draft.presetId)
        val current = active
        val address = if (!start) {
            null
        } else if (current?.endpoint == endpoint && current.preset == preset) {
            current.url
        } else {
            replace(endpoint, preset)
        }
        if (start && address == null) {
            mutableState.update { it.copy(banner = BannerMessage.CHANGES_FAILED) }
            return null
        }
        userEditedAddress = true
        preferences.saveTarget(draft.routerIp.trim(), preset.id)
        mutableState.update {
            it.copy(validationError = null, banner = if (announce) BannerMessage.CHANGES_SAVED else null)
        }
        return address
    }

    private fun replace(endpoint: Endpoint, preset: HardwarePreset): String? {
        var sessionUrl = ""
        val session = sessionFactory.create(
            endpoint = endpoint,
            preset = preset,
            language = { mutableState.value.language },
            message = message,
            telemetry = { value ->
                mutableState.update { if (it.activeUrl == sessionUrl) it.copy(tlsInfo = value) else it }
            },
            log = { line ->
                if (mutableState.value.activeUrl == sessionUrl) {
                    mutableLogs.update { (it + line).takeLast(150) }
                }
            }
        ) ?: return null
        sessionUrl = session.url
        val old = active
        active = session
        mutableLogs.value = listOf("[PROXY] ${endpoint.authority}")
        mutableState.update {
            it.copy(
                activeUrl = sessionUrl,
                activeTarget = endpoint.authority,
                tlsInfo = null
            )
        }
        old?.let(::retire)
        session.start()
        serviceController.start(endpoint.authority)
        return sessionUrl
    }

    @Synchronized
    fun stop() {
        val current = active
        active = null
        mutableState.update {
            it.copy(activeUrl = null, activeTarget = null, tlsInfo = null, banner = null)
        }
        serviceController.stop()
        current?.let(::retire)
    }

    private fun retire(session: ProxySession) {
        session.close()
        cookieCleaner.clear(session.url, session.retiredCookies())
    }
}
