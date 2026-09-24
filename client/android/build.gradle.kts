// ndkForEveryModule : The one NDK this project builds against.
//
// whisper_ggml compiles whisper.cpp and pins this version; every other
// plugin asks for whatever Flutter currently recommends. Left alone, Gradle
// honours both and downloads two NDKs of about two and a half gigabytes
// each, for one of which nothing native is ever built. Only whisper_ggml
// has C to compile, so its version is the one that matters and the rest are
// held to it.
//
// Raise this when whisper_ggml raises it; a plugin that genuinely needs a
// different one will fail loudly at link time rather than silently.
val ndkForEveryModule by extra("29.0.13113456")

allprojects {
    repositories {
        google()
        mavenCentral()
    }
}

// friday.abi : Which processor architectures to compile native code for,
// comma separated, or absent for all of them.
//
// whisper.cpp is compiled once per architecture and each one takes minutes.
// An APK built to be sideloaded onto one known phone needs exactly one, and
// building the other three triples the wait and the file for nothing.
val onlyTheseAbis: String? = providers.gradleProperty("friday.abi").orNull

// sdkForEveryModule : The Android API level every library module compiles
// against.
//
// whisper_ggml declares 34, but it depends on ffmpeg-kit, which refuses to
// be consumed by anything built against less than 35. Rather than pin the
// one plugin, every library is brought up to the level the application
// itself uses; compiling against a newer API changes no runtime behaviour,
// which is governed by targetSdk and minSdk.
val sdkForEveryModule = 36

// Applied after each module has been evaluated, not when its plugin is
// applied: a plugin's own `android { ndkVersion ... }` runs later and would
// otherwise overwrite this, which is exactly what happened the first time.
subprojects {
    afterEvaluate {
        val library = extensions
            .findByType(com.android.build.gradle.LibraryExtension::class.java)
        library?.apply {
            ndkVersion = ndkForEveryModule
            compileSdk = sdkForEveryModule
            if (!onlyTheseAbis.isNullOrBlank()) {
                defaultConfig.ndk.abiFilters.clear()
                defaultConfig.ndk.abiFilters.addAll(onlyTheseAbis.split(","))
            }
        }
    }
}

val newBuildDir: Directory =
    rootProject.layout.buildDirectory
        .dir("../../build")
        .get()
rootProject.layout.buildDirectory.value(newBuildDir)

subprojects {
    val newSubprojectBuildDir: Directory = newBuildDir.dir(project.name)
    project.layout.buildDirectory.value(newSubprojectBuildDir)
}
subprojects {
    project.evaluationDependsOn(":app")
}

tasks.register<Delete>("clean") {
    delete(rootProject.layout.buildDirectory)
}
