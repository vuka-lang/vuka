package io.github.vukalang.vuka

import com.intellij.ide.plugins.PluginManagerCore
import com.intellij.openapi.extensions.PluginId
import org.jetbrains.plugins.textmate.api.TextMateBundleProvider
import java.nio.file.Files

// VukaBundleProvider hands the IDE the TextMate bundle in the plugin's folder
// (textmate/vuka): the VS Code extension's package.json, grammars and
// snippets, copied there by the build. It highlights .vuka files, and
// .vuka.html files, which VukaHtmlFileTypeOverrider opens with it.
class VukaBundleProvider : TextMateBundleProvider {
    override fun getBundles(): List<TextMateBundleProvider.PluginBundle> {
        val plugin = PluginManagerCore.getPlugin(PluginId.getId("io.github.vuka-lang.vuka")) ?: return emptyList()
        val dir = plugin.pluginPath.resolve("textmate/vuka")
        return if (Files.isDirectory(dir)) listOf(TextMateBundleProvider.PluginBundle("Vuka", dir)) else emptyList()
    }
}
