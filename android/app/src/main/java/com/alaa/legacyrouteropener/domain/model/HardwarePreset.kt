package com.alaa.legacyrouteropener.domain.model

data class HardwarePreset(
    val id: String,
    val sockets: Int,
    val seedCookies: Map<String, String>,
    val rewriteSingleLabelHost: Boolean,
    val preferLegacyTls: Boolean
) {
    companion object {
        private val huaweiSeedCookies = mapOf(
            "FirstMenu" to "Admin_0",
            "SecondMenu" to "Admin_0_0",
            "ThirdMenu" to "Admin_0_0_0",
            "Language" to "en"
        )

        val all = listOf(
            HardwarePreset("auto", 4, huaweiSeedCookies, rewriteSingleLabelHost = true, preferLegacyTls = false),
            HardwarePreset("huawei-zte", 4, huaweiSeedCookies, rewriteSingleLabelHost = false, preferLegacyTls = true),
            HardwarePreset("idrac", 4, emptyMap(), rewriteSingleLabelHost = true, preferLegacyTls = true),
            HardwarePreset("ilo", 2, emptyMap(), rewriteSingleLabelHost = true, preferLegacyTls = true),
            HardwarePreset("apc", 2, emptyMap(), rewriteSingleLabelHost = true, preferLegacyTls = true),
            HardwarePreset("cisco-hp", 4, emptyMap(), rewriteSingleLabelHost = true, preferLegacyTls = true),
            HardwarePreset("printer-nas", 4, emptyMap(), rewriteSingleLabelHost = true, preferLegacyTls = false)
        )

        fun find(id: String?) = all.firstOrNull { it.id == id } ?: all.first()
    }
}
