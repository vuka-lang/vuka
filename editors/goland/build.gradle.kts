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
        bundledPlugin("org.jetbrains.plugins.go")
    }
    testImplementation("junit:junit:4.13.2")
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

// The highlighting and the snippets are the VS Code extension's, read from
// editors/vscode, not copied: the plugin ships its grammars as a TextMate
// bundle in its folder (textmate/vuka), which VukaBundleProvider hands to the
// IDE, and its snippets as live templates.
val vscode = layout.projectDirectory.dir("../vscode")

val liveTemplates by tasks.registering(LiveTemplates::class) {
    snippets = vscode.file("snippets/vuka.json")
    outputDir = layout.buildDirectory.dir("generated/liveTemplates")
}

sourceSets.main {
    resources.srcDir(liveTemplates)
}

tasks {
    prepareSandbox {
        from(vscode) {
            include("package.json", "language-configuration.json", "syntaxes/**", "LICENSE")
            into(intellijPlatform.projectName.map { "$it/textmate/vuka" })
        }
        // VS Code's Go grammar, which the Vuka grammars include for their Go.
        from(layout.projectDirectory.dir("textmate/go")) {
            into(intellijPlatform.projectName.map { "$it/textmate/go" })
        }
    }
}
