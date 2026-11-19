package com.alaa.legacyrouteropener.domain.model

import java.util.Locale

enum class AppLanguage(val code: String, val displayName: String, val isRtl: Boolean) {
    EN("en", "English", false),
    AR("ar", "العربية", true);

    companion object {
        fun fromCode(code: String?): AppLanguage? =
            entries.firstOrNull { it.code.equals(code, ignoreCase = true) }

        fun fromLocaleLanguage(language: String?): AppLanguage =
            fromCode(language?.lowercase(Locale.ROOT)) ?: EN
    }
}
