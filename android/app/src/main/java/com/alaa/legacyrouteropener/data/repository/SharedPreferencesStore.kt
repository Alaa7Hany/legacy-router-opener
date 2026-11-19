package com.alaa.legacyrouteropener.data.repository

import android.content.Context
import com.alaa.legacyrouteropener.domain.model.AppLanguage
import com.alaa.legacyrouteropener.domain.server.PreferencesStore

internal class SharedPreferencesStore(context: Context) : PreferencesStore {
    private val preferences = context.applicationContext.getSharedPreferences("opener", Context.MODE_PRIVATE)

    override fun loadTarget(): String? = preferences.getString(KEY_TARGET, null)

    override fun saveTarget(target: String, presetId: String) {
        preferences.edit()
            .putString(KEY_TARGET, target)
            .putString(KEY_PRESET, presetId)
            .apply()
    }

    override fun loadPresetId(): String? = preferences.getString(KEY_PRESET, null)

    override fun loadLanguage(): AppLanguage? =
        AppLanguage.fromCode(preferences.getString(KEY_LANGUAGE, null))

    override fun saveLanguage(language: AppLanguage) {
        preferences.edit().putString(KEY_LANGUAGE, language.code).apply()
    }

    private companion object {
        const val KEY_TARGET = "target"
        const val KEY_PRESET = "preset"
        const val KEY_LANGUAGE = "language"
    }
}
