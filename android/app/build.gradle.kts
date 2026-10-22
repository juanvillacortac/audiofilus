plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

// La versión: la de la etiqueta (-PversionName=0.15.0); versionCode sale de
// ella (0.15.0 -> 1500), así cada release es mayor que la anterior.
val versionNameProp = (findProperty("versionName") as String?) ?: "0.0.0"
val versionCodeProp = versionNameProp.split(".").map { it.toIntOrNull() ?: 0 }
    .let { (it.getOrElse(0) { 0 } * 10000) + (it.getOrElse(1) { 0 } * 100) + it.getOrElse(2) { 0 } }
    .coerceAtLeast(1)

android {
    namespace = "io.github.juanvillacortac.audiofilus"
    compileSdk = 36

    defaultConfig {
        applicationId = "io.github.juanvillacortac.audiofilus"
        minSdk = 24
        targetSdk = 36
        versionCode = versionCodeProp
        versionName = versionNameProp
        ndk { abiFilters += listOf("arm64-v8a", "x86_64") }
    }

    // La firma de la release: un almacén de claves que pasa CI por
    // variables de entorno. Sin él, la release sale firmada con la clave de
    // depuración (para probar).
    val ks = System.getenv("AUDIOFILUS_KEYSTORE")
    signingConfigs {
        if (ks != null) {
            create("release") {
                storeFile = file(ks)
                storePassword = System.getenv("AUDIOFILUS_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("AUDIOFILUS_KEY_ALIAS")
                keyPassword = System.getenv("AUDIOFILUS_KEY_PASSWORD")
            }
        }
    }
    // Dos versiones de la misma app: la de GitHub se actualiza sola (Ajustes →
    // Actualizar, con el permiso de instalar paquetes); la de Google Play no
    // puede pedir ese permiso y la actualiza la tienda.
    flavorDimensions += "store"
    productFlavors {
        create("github") { dimension = "store" }
        create("play") { dimension = "store" }
    }
    buildTypes {
        release {
            isMinifyEnabled = false
            signingConfig = if (ks != null) signingConfigs.getByName("release") else signingConfigs.getByName("debug")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
    packaging { jniLibs { useLegacyPackaging = true } }
}

dependencies {
    implementation(fileTree("libs") { include("*.aar") })
}
