package io.github.vukalang.vuka

import com.intellij.openapi.actionSystem.ActionUpdateThread
import com.intellij.openapi.actionSystem.AnActionEvent
import com.intellij.openapi.project.DumbAwareAction
import com.intellij.platform.lsp.api.LspServerManager

// RestartServerAction is Vuka: Restart Language Server, after installing a
// new vuka or gopls.
class RestartServerAction : DumbAwareAction() {
    override fun getActionUpdateThread() = ActionUpdateThread.BGT

    override fun update(e: AnActionEvent) {
        e.presentation.isEnabledAndVisible = e.project != null
    }

    override fun actionPerformed(e: AnActionEvent) {
        val project = e.project ?: return
        LspServerManager.getInstance(project).stopAndRestartIfNeeded(VukaLspServerSupportProvider::class.java)
    }
}
