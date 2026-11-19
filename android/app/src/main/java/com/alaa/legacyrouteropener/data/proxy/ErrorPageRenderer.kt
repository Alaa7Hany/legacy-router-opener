package com.alaa.legacyrouteropener.data.proxy

import com.alaa.legacyrouteropener.domain.model.AppLanguage

internal class ErrorPageRenderer(private val message: (AppLanguage, String) -> String) {
    fun render(language: AppLanguage, address: String): ByteArray {
        val direction = if (language.isRtl) "rtl" else "ltr"
        fun copy(key: String) = escape(message(language, key))
        return """<!doctype html>
            <html lang="${language.code}" dir="$direction"><head><meta charset="utf-8">
            <meta name="viewport" content="width=device-width,initial-scale=1">
            <title>${copy("error_title")}</title>
            <style>
            :root{color-scheme:light dark;--bg:#f4f3ef;--card:#fff;--border:#d6d3cd;--text:#1c1917;--link:#1d4ed8}
            @media(prefers-color-scheme:dark){:root{--bg:#18181b;--card:#222226;--border:#3f3f46;--text:#f4f4f5;--link:#93c5fd}}
            body{margin:0;padding:24px 16px;min-height:100vh;min-height:100dvh;box-sizing:border-box;display:grid;place-items:center;
            background:var(--bg);color:var(--text);font:16px/1.5 system-ui,sans-serif}
            main{width:100%;max-width:420px;box-sizing:border-box;border:1px solid var(--border);
            border-radius:12px;padding:24px;background:var(--card)}
            h1{font-size:20px;margin:0}a{color:var(--link)}details{margin-top:16px}bdi{overflow-wrap:anywhere}
            </style></head><body><main>
            <h1>${copy("error_title")}</h1><p>${copy("error_help")}</p>
            <a href="">${copy("try_again")}</a>
            <details><summary>${copy("technical_details")}</summary><p><bdi dir="ltr">502 · ${escape(address)}</bdi></p></details>
            </main></body></html>""".trimIndent().toByteArray(Charsets.UTF_8)
    }

    private fun escape(text: String) = text.replace("&", "&amp;").replace("<", "&lt;")
        .replace(">", "&gt;").replace("\"", "&quot;").replace("'", "&#39;")
}
