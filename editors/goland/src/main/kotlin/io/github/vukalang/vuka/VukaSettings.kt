package io.github.vukalang.vuka

import com.intellij.openapi.components.BaseState
import com.intellij.openapi.components.Service
import com.intellij.openapi.components.SimplePersistentStateComponent
import com.intellij.openapi.components.State
import com.intellij.openapi.components.Storage
import com.intellij.openapi.components.service
import com.intellij.openapi.fileEditor.FileEditorManager
import com.intellij.openapi.options.BoundConfigurable
import com.intellij.openapi.project.ProjectManager
import com.intellij.openapi.ui.DialogPanel
import com.intellij.platform.lsp.api.LspServerManager
import com.intellij.ui.dsl.builder.bindSelected
import com.intellij.ui.dsl.builder.bindText
import com.intellij.ui.dsl.builder.columns
import com.intellij.ui.dsl.builder.COLUMNS_LARGE
import com.intellij.ui.dsl.builder.panel
import com.intellij.util.FileContentUtilCore

// VukaSettings are the IDE-wide settings, the VS Code extension's vuka.path,
// vuka.goplsPath, vuka.sharedGopls, vuka.autoCloseTags and formatOnSave,
// and the two GoLand needs: how .vuka.html files open, and whether saving a
// .vuka file writes its Go.
@Service(Service.Level.APP)
@State(name = "VukaSettings", storages = [Storage("vuka.xml")])
class VukaSettings : SimplePersistentStateComponent<VukaSettings.Settings>(Settings()) {
    class Settings : BaseState() {
        var vukaPath by string()
        var goplsPath by string()
        var sharedGopls by property(true)
        var templateGrammar by property(true)
        var genOnSave by property(false)
        var autoCloseTags by property(true)
        var formatOnSave by property(true)
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
        row("Running:") {
            label(VukaVersion.running ?: "not started: open a .vuka file")
        }
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
        row {
            checkBox("Format .vuka and .vuka.html files on save")
                .bindSelected({ settings.formatOnSave }, { settings.formatOnSave = it })
        }
        row {
            checkBox("Close JSX tags as they are typed")
                .bindSelected({ settings.autoCloseTags }, { settings.autoCloseTags = it })
                .comment("An opening tag's &gt; adds its closing tag, and &lt;/ the name of the element still open.")
        }
        row {
            checkBox("Highlight .vuka.html files with the Vuka grammar")
                .bindSelected({ settings.templateGrammar }, { settings.templateGrammar = it })
                .comment("Off: they open as HTML, the Go in them still served by vuka lsp.")
        }
        row {
            checkBox("Write the Go of .vuka files beside them on save (vuka gen -inplace)")
                .bindSelected({ settings.genOnSave }, { settings.genOnSave = it })
                .comment("GoLand reads Go from disk only: this lets .go files see what .vuka files declare.")
        }
    }

    override fun apply() {
        super.apply()
        for (project in ProjectManager.getInstance().openProjects) {
            LspServerManager.getInstance(project).stopAndRestartIfNeeded(VukaLspServerSupportProvider::class.java)
            // Open templates take their new file type.
            FileContentUtilCore.reparseFiles(FileEditorManager.getInstance(project).openFiles.filter { it.name.endsWith(".vuka.html") })
        }
    }
}
