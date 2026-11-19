package com.alaa.legacyrouteropener.domain.model

enum class ValidationError(val messageKey: String) {
    ADDRESS_REQUIRED("address_required"),
    ADDRESS_SPACES("address_spaces"),
    ADDRESS_INVALID("address_invalid"),
    ADDRESS_SELF_BLOCKED("address_self_blocked")
}

enum class BannerMessage(val messageKey: String, val isError: Boolean) {
    CHANGES_SAVED("changes_saved", isError = false),
    CHANGES_FAILED("changes_failed", isError = true),
    DETECTED_SAVED("detected_saved", isError = false),
    DETECTION_FAILED("detection_failed", isError = true),
    LOGIN_FIXED("login_fixed", isError = false),
    LOGIN_FIX_FAILED("login_fix_failed", isError = true),
    BROWSER_UNAVAILABLE("browser_unavailable", isError = true)
}
