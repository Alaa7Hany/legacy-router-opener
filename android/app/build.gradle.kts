import org.objectweb.asm.ClassReader
import org.objectweb.asm.ClassVisitor
import org.objectweb.asm.ClassWriter
import org.objectweb.asm.Label
import org.objectweb.asm.MethodVisitor
import org.objectweb.asm.Opcodes.*
import java.util.zip.ZipEntry
import java.util.zip.ZipFile
import java.util.zip.ZipOutputStream

plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

val legacyTlsArtifact by configurations.creating {
    isTransitive = false
    isCanBeConsumed = false
}
val legacyTlsJar = layout.buildDirectory.file("patched-libraries/bctls-legacy-1.78.1.jar")
val patchLegacyTls = tasks.register("patchLegacyTls") {
    inputs.files(legacyTlsArtifact)
    outputs.file(legacyTlsJar)
    doLast {
        val output = legacyTlsJar.get().asFile
        output.parentFile.mkdirs()
        ZipFile(legacyTlsArtifact.singleFile).use { source ->
            ZipOutputStream(output.outputStream().buffered()).use { target ->
                var patched = 0
                source.entries().asSequence().forEach { entry ->
                    // The source signature covers the unmodified archive.
                    if (entry.name.matches(Regex("META-INF/[^/]+\\.(SF|DSA|RSA|EC)", RegexOption.IGNORE_CASE))) {
                        return@forEach
                    }
                    target.putNextEntry(ZipEntry(entry.name).apply { time = 0 })
                    source.getInputStream(entry).use { content ->
                        if (entry.name.endsWith("org/bouncycastle/tls/TlsUtils.class")) {
                            target.write(restoreLegacyRsaRc4Mappings(content.readBytes()))
                            patched++
                        } else {
                            content.copyTo(target)
                        }
                    }
                    target.closeEntry()
                }
                check(patched == 2) { "Expected base and Java 9 TlsUtils classes in Bouncy Castle 1.78.1" }
            }
        }
    }
}

// BC 1.78.1 still supports SSLv3 and stream records but omits the two RSA/RC4 suite
// mappings from TlsUtils. The app advertises these suites only in its legacy profiles.
fun restoreLegacyRsaRc4Mappings(bytes: ByteArray): ByteArray {
    val reader = ClassReader(bytes)
    val writer = ClassWriter(reader, ClassWriter.COMPUTE_MAXS)
    val mappings = mapOf(
        "getKeyExchangeAlgorithm" to (1 to 1), // RSA
        "getEncryptionAlgorithm" to (2 to 2), // RC4_128
        "getMACAlgorithm" to (1 to 2) // HMAC MD5, HMAC SHA-1
    )
    val patched = mutableSetOf<String>()
    reader.accept(object : ClassVisitor(ASM9, writer) {
        override fun visitMethod(access: Int, name: String, descriptor: String, signature: String?,
            exceptions: Array<out String>?): MethodVisitor {
            val delegate = super.visitMethod(access, name, descriptor, signature, exceptions)
            val values = mappings[name] ?: return delegate
            if (descriptor != "(I)I") return delegate
            patched.add(name)
            return object : MethodVisitor(ASM9, delegate) {
                override fun visitCode() {
                    super.visitCode()
                    for ((suite, value) in listOf(4 to values.first, 5 to values.second)) {
                        val next = Label()
                        visitVarInsn(ILOAD, 0)
                        visitLdcInsn(suite)
                        visitJumpInsn(IF_ICMPNE, next)
                        visitLdcInsn(value)
                        visitInsn(IRETURN)
                        visitLabel(next)
                        visitFrame(F_SAME, 0, null, 0, null)
                    }
                }
            }
        }
    }, 0)
    check(patched == mappings.keys) { "Unexpected Bouncy Castle cipher table" }
    return writer.toByteArray()
}

android {
    namespace = "com.alaa.legacyrouteropener"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.alaa.legacyrouteropener"
        minSdk = 26
        targetSdk = 35
        versionCode = 1
        versionName = "1.0.0"

        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
        vectorDrawables {
            useSupportLibrary = true
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    packaging {
        resources {
            excludes += setOf(
                "/META-INF/{AL2.0,LGPL2.1}",
                "META-INF/versions/9/OSGI-INF/MANIFEST.MF",
                "META-INF/INDEX.LIST",
                "META-INF/io.netty.versions.properties"
            )
        }
    }
}

dependencies {
    // AndroidX Core & Lifecycle
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.8.5")
    implementation("androidx.activity:activity-compose:1.9.2")

    // Jetpack Compose
    val composeBom = platform("androidx.compose:compose-bom:2024.09.00")
    implementation(composeBom)
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-graphics")
    implementation("androidx.compose.ui:ui-tooling-preview")
    implementation("androidx.compose.material3:material3")

    // Coroutines
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.8.1")

    // Bouncy Castle for Legacy TLS 1.0/1.1 & obsolete cipher support on Android 10+
    implementation("org.bouncycastle:bcprov-jdk18on:1.78.1")
    implementation("org.bouncycastle:bcutil-jdk18on:1.78.1")
    legacyTlsArtifact("org.bouncycastle:bctls-jdk18on:1.78.1")
    implementation(files(legacyTlsJar).builtBy(patchLegacyTls))

    testImplementation("junit:junit:4.13.2")
    testImplementation("org.bouncycastle:bcpkix-jdk18on:1.78.1")

    // Debug tooling
    debugImplementation("androidx.compose.ui:ui-tooling")
    debugImplementation("androidx.compose.ui:ui-test-manifest")
}

tasks.register<JavaExec>("legacyRouterFixtures") {
    description = "Starts loopback legacy HTTPS fixtures for device smoke tests over adb reverse."
    val debugTests = tasks.named<Test>("testDebugUnitTest")
    dependsOn("compileDebugUnitTestKotlin")
    classpath = debugTests.get().classpath
    mainClass.set("com.alaa.legacyrouteropener.LegacyRouterFixtures")
}
