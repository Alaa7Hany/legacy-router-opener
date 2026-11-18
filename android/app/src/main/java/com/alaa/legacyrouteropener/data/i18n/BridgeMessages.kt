package com.alaa.legacyrouteropener.data.i18n

import android.content.Context
import org.json.JSONObject

class BridgeMessages(context: Context) {
    private val catalog = context.assets.open("messages.json").bufferedReader().use { JSONObject(it.readText()) }

    operator fun get(language: String, key: String): String =
        catalog.optJSONObject(language)?.optString(key)?.takeIf { it.isNotEmpty() }
            ?: catalog.getJSONObject("en").optString(key, key)
}
