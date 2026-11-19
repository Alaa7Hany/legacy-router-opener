package com.alaa.legacyrouteropener.data.i18n

import android.content.Context
import com.alaa.legacyrouteropener.domain.model.AppLanguage
import org.json.JSONObject

class BridgeMessages(context: Context) {
    private val appContext = context.applicationContext
    private val catalog: JSONObject by lazy(LazyThreadSafetyMode.SYNCHRONIZED) {
        appContext.assets.open("messages.json").bufferedReader().use { JSONObject(it.readText()) }
    }

    operator fun get(language: AppLanguage, key: String): String = get(language.code, key)

    operator fun get(language: String, key: String): String =
        catalog.optJSONObject(language)?.optString(key)?.takeIf { it.isNotEmpty() }
            ?: catalog.getJSONObject(AppLanguage.EN.code).optString(key, key)
}
