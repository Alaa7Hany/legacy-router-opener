package com.alaa.legacyrouteropener.data.model

data class HardwarePreset(
    val id: String,
    val defaultTarget: String?,
    val sockets: Int,
    val seedHuawei: Boolean,
    val rewriteSingleLabelHost: Boolean,
    val preferLegacyTls: Boolean
) {
    companion object {
        val all = listOf(
            HardwarePreset("auto", null, 4, true, true, false),
            HardwarePreset("huawei-zte", "192.168.1.1", 4, true, false, true),
            HardwarePreset("idrac", "192.168.0.120", 4, false, true, true),
            HardwarePreset("ilo", "192.168.1.100", 2, false, true, true),
            HardwarePreset("apc", "192.168.1.50", 2, false, true, true),
            HardwarePreset("cisco-hp", "192.168.1.254", 4, false, true, true),
            HardwarePreset("printer-nas", "192.168.1.200", 4, false, true, false)
        )

        fun find(id: String?) = all.firstOrNull { it.id == id } ?: all.first()
    }
}
