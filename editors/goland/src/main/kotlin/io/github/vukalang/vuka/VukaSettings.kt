package io.github.vukalang.vuka

import com.intellij.openapi.components.BaseState
import com.intellij.openapi.components.Service
import com.intellij.openapi.components.SimplePersistentStateComponent
import com.intellij.openapi.components.State
import com.intellij.openapi.components.Storage
import com.intellij.openapi.components.service
import com.intellij.openapi.options.BoundConfigurable
import com.intellij.openapi.project.ProjectManager
import com.intellij.openapi.ui.DialogPanel
import com.intellij.platform.lsp.api.LspServerManager
import com.intellij.ui.dsl.builder.bindSelected
import com.intellij.ui.dsl.builder.bindText
import com.intellij.ui.dsl.builder.columns
import com.intellij.ui.dsl.builder.COLUMNS_LARGE
import com.intellij.ui.dsl.builder.panel

// VukaSettings are the IDE-wide settings, the VS Code extension's vuka.path,
// vuka.goplsPath and vuka.sharedGopls.
@Service(Service.Level.APP)
@State(name = "VukaSettings", storages = [Storage("vuka.xml")])
class VukaSettings : SimplePersistentStateComponent<VukaSettings.Settings>(Settings()) {
    class Settings : BaseState() {
        var vukaPath by string()
        var goplsPath by string()
        var sharedGopls by property(true)
    }

    companion object {
        fun getInstance(): VukaSettings = service()
    }
}

// VukaConfigurable is Settings | Languages & Frameworks | Vuka. Applying a
// change restarts the server in every open project.
class VukaConfigurable : BoundConfigurable("Vuka") {
    private val settings get() = VukaSettings.getInstance().state

    override fun createPanel(): DialogPanel = panel {
        row("vuka binary:") {
            textField()
                .columns(COLUMNS_LARGE)
                .bindText({ settings.vukaPath ?: "" }, { settings.vukaPath = it.trim().ifEmpty { null } })
                .comment("Empty: go tool vuka when go.mod declares it, else vuka on PATH, \$GOBIN or \$GOPATH/bin.")
        }
        row("gopls binary:") {
            textField()
                .columns(COLUMNS_LARGE)
                .bindText({ settings.goplsPath ?: "" }, { settings.goplsPath = it.trim().ifEmpty { null } })
                .comment("Empty: gopls on PATH, then \$GOBIN, then \$GOPATH/bin.")
        }
        row {
            checkBox("Share one gopls daemon (gopls -remote=auto)")
                .bindSelected({ settings.sharedGopls }, { settings.sharedGopls = it })
        }
    }

    override fun apply() {
        super.apply()
        for (project in ProjectManager.getInstance().openProjects) {
            LspServerManager.getInstance(project).stopAndRestartIfNeeded(VukaLspServerSupportProvider::class.java)
        }
    }
}
