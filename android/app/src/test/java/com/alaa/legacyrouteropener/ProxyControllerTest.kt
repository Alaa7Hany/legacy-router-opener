package com.alaa.legacyrouteropener

import com.alaa.legacyrouteropener.domain.model.AppLanguage
import com.alaa.legacyrouteropener.domain.model.BannerMessage
import com.alaa.legacyrouteropener.domain.model.Endpoint
import com.alaa.legacyrouteropener.domain.model.HardwarePreset
import com.alaa.legacyrouteropener.domain.model.TlsInfo
import com.alaa.legacyrouteropener.domain.model.ValidationError
import com.alaa.legacyrouteropener.domain.server.GatewayProvider
import com.alaa.legacyrouteropener.domain.server.PreferencesStore
import com.alaa.legacyrouteropener.domain.server.ProxyController
import com.alaa.legacyrouteropener.domain.server.ProxyServiceController
import com.alaa.legacyrouteropener.domain.server.ProxySession
import com.alaa.legacyrouteropener.domain.server.ProxySessionFactory
import com.alaa.legacyrouteropener.domain.server.SessionCookieCleaner
import org.junit.Assert.*
import org.junit.Test

class ProxyControllerTest {
    private class FakePreferences(
        var target: String? = null,
        var presetId: String? = null,
        var language: AppLanguage? = null
    ) : PreferencesStore {
        override fun loadTarget() = target
        override fun saveTarget(target: String, presetId: String) {
            this.target = target
            this.presetId = presetId
        }
        override fun loadPresetId() = presetId
        override fun loadLanguage() = language
        override fun saveLanguage(language: AppLanguage) { this.language = language }
    }

    private class FakeGateway(var gateway: String? = null) : GatewayProvider {
        override fun detectGatewayIp() = gateway
    }

    private class FakeServiceController : ProxyServiceController {
        val started = mutableListOf<String>()
        var stops = 0
        override fun start(target: String) { started += target }
        override fun stop() { stops++ }
    }

    private class FakeCookieCleaner : SessionCookieCleaner {
        val cleared = mutableListOf<Pair<String, List<Pair<String, String>>>>()
        override fun clear(url: String, cookies: List<Pair<String, String>>) {
            cleared += url to cookies
        }
    }

    private class FakeSession(
        override val endpoint: Endpoint,
        override val preset: HardwarePreset,
        override val url: String,
        val emitLog: (String) -> Unit,
        val cookies: List<Pair<String, String>> = listOf("sid" to "/")
    ) : ProxySession {
        var started = false
        var closed = false
        override fun start() { started = true }
        override fun retiredCookies() = cookies
        override fun close() { closed = true }
    }

    private class FakeSessionFactory(var shouldFail: Boolean = false) : ProxySessionFactory {
        val created = mutableListOf<FakeSession>()
        override fun create(
            endpoint: Endpoint,
            preset: HardwarePreset,
            language: () -> AppLanguage,
            message: (AppLanguage, String) -> String,
            telemetry: (TlsInfo) -> Unit,
            log: (String) -> Unit
        ): ProxySession? {
            if (shouldFail) return null
            val index = created.size + 1
            return FakeSession(endpoint, preset, "http://session$index.localhost:8080/", log).also {
                created += it
            }
        }
    }

    private fun controller(
        prefs: FakePreferences = FakePreferences(),
        gateway: FakeGateway = FakeGateway("192.168.1.1"),
        service: FakeServiceController = FakeServiceController(),
        cookies: FakeCookieCleaner = FakeCookieCleaner(),
        factory: FakeSessionFactory = FakeSessionFactory()
    ) = ProxyController(prefs, gateway, service, cookies, factory, AppLanguage.EN) { _, key -> key }

    @Test fun validationMapsToTypedErrorsWithoutStartingSession() {
        val factory = FakeSessionFactory()
        val ctrl = controller(factory = factory)

        ctrl.changeAddress("   ")
        assertNull(ctrl.open())
        assertEquals(ValidationError.ADDRESS_REQUIRED, ctrl.state.value.validationError)

        ctrl.changeAddress("192.168.1.1 80")
        assertNull(ctrl.open())
        assertEquals(ValidationError.ADDRESS_SPACES, ctrl.state.value.validationError)

        ctrl.changeAddress("sub.localhost:8080")
        assertNull(ctrl.open())
        assertEquals(ValidationError.ADDRESS_SELF_BLOCKED, ctrl.state.value.validationError)

        assertTrue(factory.created.isEmpty())
    }

    @Test fun openReusesMatchingSessionAndRetiresOnChangeOrReset() {
        val service = FakeServiceController()
        val cookies = FakeCookieCleaner()
        val factory = FakeSessionFactory()
        val ctrl = controller(service = service, cookies = cookies, factory = factory)

        val firstUrl = ctrl.open()
        assertEquals("http://session1.localhost:8080/", firstUrl)
        assertTrue(factory.created[0].started)
        assertEquals(listOf("192.168.1.1"), service.started)

        // Calling open again with unchanged target and preset reuses the active session.
        assertEquals(firstUrl, ctrl.open())
        assertEquals(1, factory.created.size)

        // Resetting login creates a new session and retires the previous one.
        ctrl.resetLogin()
        assertEquals(BannerMessage.LOGIN_FIXED, ctrl.state.value.banner)
        assertEquals("http://session2.localhost:8080/", ctrl.state.value.activeUrl)
        assertTrue(factory.created[0].closed)
        assertEquals(1, cookies.cleared.size)
        assertEquals(firstUrl, cookies.cleared[0].first)

        // Stopping retires the active session and stops the service.
        ctrl.stop()
        assertFalse(ctrl.state.value.isRunning)
        assertTrue(factory.created[1].closed)
        assertEquals(1, service.stops)
        assertEquals(2, cookies.cleared.size)
    }

    @Test fun refreshGatewayUpdatesUneditedTargetButPreservesUserEdit() {
        val gateway = FakeGateway("192.168.0.1")
        val ctrl = controller(gateway = gateway)

        ctrl.refreshGateway()
        assertEquals("192.168.0.1", ctrl.state.value.detectedGateway)
        assertEquals("192.168.0.1", ctrl.state.value.routerIp)

        ctrl.changeAddress("10.0.0.1")
        gateway.gateway = "192.168.100.1"
        ctrl.refreshGateway()
        assertEquals("192.168.100.1", ctrl.state.value.detectedGateway)
        assertEquals("10.0.0.1", ctrl.state.value.routerIp)

        ctrl.useDetected()
        assertEquals("192.168.100.1", ctrl.state.value.routerIp)
        assertEquals(BannerMessage.DETECTED_SAVED, ctrl.state.value.banner)
    }

    @Test fun logsUpdateIndependentlyFromProxyState() {
        val factory = FakeSessionFactory()
        val ctrl = controller(factory = factory)
        ctrl.open()
        val stateBefore = ctrl.state.value

        factory.created[0].emitLog("[GET] /status")
        assertSame(stateBefore, ctrl.state.value)
        assertEquals(listOf("[PROXY] 192.168.1.1", "[GET] /status"), ctrl.logs.value)

        ctrl.clearLogs()
        assertTrue(ctrl.logs.value.isEmpty())
    }
}
