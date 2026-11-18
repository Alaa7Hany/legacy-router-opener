# Connection details derive standard cipher names from Bouncy Castle's constants.
-keepclassmembers class org.bouncycastle.tls.CipherSuite {
    public static final int TLS_*;
}
