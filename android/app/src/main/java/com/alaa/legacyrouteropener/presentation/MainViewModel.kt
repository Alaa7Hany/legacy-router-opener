package com.alaa.legacyrouteropener.presentation

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.alaa.legacyrouteropener.domain.model.AppLanguage
import com.alaa.legacyrouteropener.domain.model.HardwarePreset
import com.alaa.legacyrouteropener.domain.server.ProxyController
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext

class MainViewModel(private val controller: ProxyController) : ViewModel() {
    val state = controller.state
    val logs = controller.logs

    fun updateRouterIp(ip: String) = controller.changeAddress(ip)
    fun choosePreset(preset: HardwarePreset) = controller.choosePreset(preset)
    fun chooseLanguage(language: AppLanguage) = controller.chooseLanguage(language)
    fun useDetected() = controller.useDetected()
    fun clearLogs() = controller.clearLogs()
    fun refreshGateway() { viewModelScope.launch(Dispatchers.IO) { controller.refreshGateway() } }
    suspend fun startProxy(): String? = withContext(Dispatchers.IO) { controller.open() }
    fun stopProxy() { viewModelScope.launch(Dispatchers.IO) { controller.stop() } }
    fun applyChanges() { viewModelScope.launch(Dispatchers.IO) { controller.applyChanges() } }
    fun resetLogin() { viewModelScope.launch(Dispatchers.IO) { controller.resetLogin() } }
}
