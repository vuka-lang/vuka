package io.github.vukalang.vuka

import com.intellij.ide.plugins.PluginManagerCore
import com.intellij.openapi.application.PathManager
import com.intellij.openapi.diagnostic.logger
import com.intellij.openapi.extensions.PluginId
import org.jetbrains.plugins.textmate.api.TextMateBundleProvider
import java.nio.file.FileSystems
import java.nio.file.Files
import java.nio.file.Path
import java.nio.file.StandardCopyOption
import kotlin.io.path.isDirectory
import kotlin.io.path.relativeTo

// VukaBundleProvider hands the IDE the TextMate bundles the build puts in
// the plugin's jar, unpacked into the IDE's system folder, since the IDE
// reads a bundle from a folder: textmate/vuka, the VS Code extension's
// package.json and grammars, which highlight .vuka files and the .vuka.html
// files VukaHtmlFileTypeOverrider opens with them; and textmate/go, VS Code's
// Go grammar (source.go), which they include to color their Go.
class VukaBundleProvider : TextMateBundleProvider {
    override fun getBundles(): List<TextMateBundleProvider.PluginBundle> {
        val root = try {
            unpack()
        } catch (e: Exception) {
            log.warn("couldn't unpack the Vuka TextMate bundles", e)
            return emptyList()
        }
        return listOf("Vuka" to "vuka", "Go for Vuka" to "go")
            .map { (name, dir) -> name to root.resolve(dir) }
            .filter { (_, dir) -> Files.isRegularFile(dir.resolve("package.json")) }
            .map { (name, dir) -> TextMateBundleProvider.PluginBundle(name, dir) }
    }

    // unpack copies the jar's textmate folder to the system folder, under the
    // plugin's version, and returns the copy.
    private fun unpack(): Path {
        val version = PluginManagerCore.getPlugin(PluginId.getId("io.github.vuka-lang.vuka"))?.version ?: "dev"
        val target = Path.of(PathManager.getSystemPath(), "vuka", "textmate-$version")
        val location = Path.of(VukaBundleProvider::class.java.protectionDomain.codeSource.location.toURI())
        if (location.isDirectory()) {
            copyTree(location.resolve("textmate"), target)
        } else {
            FileSystems.newFileSystem(location).use { jar -> copyTree(jar.getPath("/textmate"), target) }
        }
        return target
    }

    private fun copyTree(from: Path, to: Path) {
        Files.walk(from).use { paths ->
            for (path in paths.filter(Files::isRegularFile)) {
                val dest = to.resolve(path.relativeTo(from).toString())
                Files.createDirectories(dest.parent)
                Files.copy(path, dest, StandardCopyOption.REPLACE_EXISTING)
            }
        }
    }

    companion object {
        private val log = logger<VukaBundleProvider>()
    }
}
