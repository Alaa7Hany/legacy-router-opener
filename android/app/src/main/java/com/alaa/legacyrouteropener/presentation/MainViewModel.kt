package com.alaa.legacyrouteropener.presentation

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import com.alaa.legacyrouteropener.LegacyRouterApplication
import com.alaa.legacyrouteropener.data.model.HardwarePreset
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

class MainViewModel(application: Application) : AndroidViewModel(application) {
    private val controller = (application as LegacyRouterApplication).controller
    val state = controller.state
    fun updateRouterIp(ip: String) = controller.changeAddress(ip)
    fun choosePreset(preset: HardwarePreset) = controller.choosePreset(preset)
    fun chooseLanguage(language: String) = controller.chooseLanguage(language)
    fun useDetected() = controller.useDetected()
    suspend fun startProxy(): String? = withContext(Dispatchers.IO) { controller.open() }
    fun stopProxy() { viewModelScope.launch(Dispatchers.IO) { controller.stop() } }
    fun applyChanges() { viewModelScope.launch(Dispatchers.IO) { controller.applyChanges() } }
    fun resetLogin() { viewModelScope.launch(Dispatchers.IO) { controller.resetLogin() } }
    fun clearLogs() = controller.clearLogs()
}
