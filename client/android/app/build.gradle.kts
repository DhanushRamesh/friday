plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

android {
    namespace = "com.dhanushramesh.friday_client"
    compileSdk = flutter.compileSdkVersion
    // whisper_ggml compiles whisper.cpp, so a real NDK is needed. The
    // version is the one that plugin pins; see the root build file for why
    // every module is held to it rather than to Flutter's default.
    ndkVersion = rootProject.extra["ndkForEveryModule"] as String

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        // TODO: Specify your own unique Application ID (https://developer.android.com/studio/build/application-id.html).
        applicationId = "com.dhanushramesh.friday_client"
        // You can update the following values to match your application needs.
        // For more information, see: https://flutter.dev/to/review-gradle-config.
        minSdk = flutter.minSdkVersion
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName

        // Packaged code is filtered as well as compiled code. Some
        // dependencies — ffmpeg-kit, which whisper_ggml pulls in — ship
        // prebuilt libraries for every architecture, and those are copied
        // in whatever was compiled. Fifty megabytes of the APK was machine
        // code for processors the phone does not have.
        val onlyTheseAbis = providers.gradleProperty("friday.abi").orNull
        if (!onlyTheseAbis.isNullOrBlank()) {
            ndk.abiFilters.clear()
            ndk.abiFilters.addAll(onlyTheseAbis.split(","))
        }
    }

    buildTypes {
        release {
            // TODO: Add your own signing config for the release build.
            // Signing with the debug keys for now, so `flutter run --release` works.
            signingConfig = signingConfigs.getByName("debug")
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}
