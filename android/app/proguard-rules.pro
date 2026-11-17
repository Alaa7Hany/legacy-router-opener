# NanoHTTPD rules
-keep class fi.iki.elonen.** { *; }

# Bouncy Castle rules
-keep class org.bouncycastle.** { *; }
-dontwarn org.bouncycastle.**

# OkHttp rules
-dontwarn okhttp3.**
-dontwarn okio.**
-dontwarn javax.annotation.**
-keepnames class okhttp3.internal.publicsuffix.PublicSuffixDatabase
