package io.github.vukalang.vuka

import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.platform.lsp.api.LspServerSupportProvider
import com.intellij.platform.lsp.api.ProjectWideLspServerDescriptor

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
}

// VukaLspServerDescriptor is vuka lsp over stdio: gopls behind a proxy that
// keeps the Go generated from each .vuka file open in gopls, never on disk.
class VukaLspServerDescriptor(project: Project) : ProjectWideLspServerDescriptor(project, "Vuka") {
    override fun isSupportedFile(file: VirtualFile): Boolean = isVukaFile(file)

    // The ids the VS Code extension sends; vuka lsp tells the two kinds of
    // file apart by their names, but other servers it starts may not.
    override fun getLanguageId(file: VirtualFile): String =
        if (file.name.endsWith(".vuka.html")) "vuka-html" else "vuka"

    override fun createCommandLine(): GeneralCommandLine = vukaLspCommandLine(project)
}
