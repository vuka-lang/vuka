plugins {
    id("org.jetbrains.kotlin.jvm") version "2.1.20"
    id("org.jetbrains.intellij.platform") version "2.7.2"
}

group = "io.github.vuka-lang"
version = providers.gradleProperty("pluginVersion").get()

kotlin {
    jvmToolchain(21)
}

repositories {
    mavenCentral()
    intellijPlatform {
        defaultRepositories()
    }
}

dependencies {
    intellijPlatform {
        goland(providers.gradleProperty("platformVersion"))
        bundledPlugin("org.jetbrains.plugins.textmate")
    }
}

intellijPlatform {
    pluginConfiguration {
        version = providers.gradleProperty("pluginVersion")
        ideaVersion {
            sinceBuild = providers.gradleProperty("pluginSinceBuild")
            untilBuild = provider { null }
        }
    }
}

// The highlighting is the VS Code extension's TextMate grammars, read from
// editors/vscode, not copied: the plugin ships them as a TextMate bundle in
// its folder (textmate/vuka), which VukaBundleProvider hands to the IDE.
val vscode = layout.projectDirectory.dir("../vscode")

tasks {
    prepareSandbox {
        from(vscode) {
            include("package.json", "language-configuration.json", "syntaxes/**", "snippets/**", "LICENSE")
            into(intellijPlatform.projectName.map { "$it/textmate/vuka" })
        }
    }
}
