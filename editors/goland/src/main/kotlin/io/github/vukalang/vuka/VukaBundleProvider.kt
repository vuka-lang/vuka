package io.github.vukalang.vuka

import com.intellij.ide.plugins.PluginManagerCore
import com.intellij.openapi.extensions.PluginId
import org.jetbrains.plugins.textmate.api.TextMateBundleProvider
import java.nio.file.Files

// VukaBundleProvider hands the IDE the TextMate bundles in the plugin's
// folder, copied there by the build: textmate/vuka, the VS Code extension's
// package.json and grammars, which highlight .vuka files and the .vuka.html
// files VukaHtmlFileTypeOverrider opens with them; and textmate/go, VS Code's
// Go grammar (source.go), which they include to color their Go. VS Code
// ships one; GoLand, with its own Go support, doesn't.
class VukaBundleProvider : TextMateBundleProvider {
    override fun getBundles(): List<TextMateBundleProvider.PluginBundle> {
        val plugin = PluginManagerCore.getPlugin(PluginId.getId("io.github.vuka-lang.vuka")) ?: return emptyList()
        return listOf("Vuka" to "textmate/vuka", "Go for Vuka" to "textmate/go")
            .map { (name, dir) -> name to plugin.pluginPath.resolve(dir) }
            .filter { (_, dir) -> Files.isDirectory(dir) }
            .map { (name, dir) -> TextMateBundleProvider.PluginBundle(name, dir) }
    }
}
