package io.github.vukalang.vuka

import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.execution.process.CapturingProcessHandler
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.platform.lsp.api.LspServer
import com.intellij.platform.lsp.api.LspServerSupportProvider
import com.intellij.platform.lsp.api.ProjectWideLspServerDescriptor
import com.intellij.platform.lsp.api.lsWidget.LspServerWidgetItem

// isVukaFile reports whether vuka lsp serves file: .vuka files and
// .vuka.html templates. Go files stay GoLand's own.
fun isVukaFile(file: VirtualFile): Boolean =
    file.isInLocalFileSystem && (file.name.endsWith(".vuka") || file.name.endsWith(".vuka.html"))

// VukaLspServerSupportProvider starts vuka lsp, one per project, when a Vuka
// file is opened.
class VukaLspServerSupportProvider : LspServerSupportProvider {
    override fun fileOpened(
        project: Project,
        file: VirtualFile,
        serverStarter: LspServerSupportProvider.LspServerStarter,
    ) {
        if (isVukaFile(file)) {
            serverStarter.ensureServerStarted(VukaLspServerDescriptor(project))
        }
    }

    // The server in the status bar's Language Services widget, with the Vuka
    // icon and a link to its settings, where the running version shows.
    override fun createLspServerWidgetItem(lspServer: LspServer, currentFile: VirtualFile?): LspServerWidgetItem =
        LspServerWidgetItem(lspServer, currentFile, VukaIcons.FILE, VukaConfigurable::class.java)
}

// VukaVersion is what `vuka version` printed when the server last started,
// and the command it ran: the VS Code extension's language status.
object VukaVersion {
    @Volatile
    var running: String? = null
}

// VukaLspServerDescriptor is vuka lsp over stdio: gopls behind a proxy that
// keeps the Go generated from each .vuka file open in gopls, never on disk.
class VukaLspServerDescriptor(project: Project) : ProjectWideLspServerDescriptor(project, "Vuka") {
    override fun isSupportedFile(file: VirtualFile): Boolean = isVukaFile(file)

    // The ids the VS Code extension sends; vuka lsp tells the two kinds of
    // file apart by their names, but other servers it starts may not.
    override fun getLanguageId(file: VirtualFile): String =
        if (file.name.endsWith(".vuka.html")) "vuka-html" else "vuka"

    override fun createCommandLine(): GeneralCommandLine {
        val command = vukaLspCommandLine(project)
        VukaVersion.running = try {
            val version = vukaCommandLine(project, listOf("version"))
            val output = CapturingProcessHandler(version).runProcess(30_000)
            "${output.stdout.trim().ifEmpty { "vuka" }} (${version.commandLineString})"
        } catch (e: Exception) {
            null
        }
        return command
    }
}
