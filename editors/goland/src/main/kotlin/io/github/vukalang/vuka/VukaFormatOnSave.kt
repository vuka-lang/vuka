package io.github.vukalang.vuka

import com.intellij.openapi.command.WriteCommandAction
import com.intellij.openapi.editor.Document
import com.intellij.openapi.fileEditor.FileDocumentManager
import com.intellij.openapi.fileEditor.FileDocumentManagerListener
import com.intellij.openapi.project.ProjectLocator
import com.intellij.psi.PsiDocumentManager
import com.intellij.psi.codeStyle.CodeStyleManager

// VukaFormatOnSave formats .vuka and .vuka.html files as they are saved, by
// vuka lsp (vuka fmt), as the VS Code extension's editor.formatOnSave
// default does. Settings | Languages & Frameworks | Vuka turns it off.
class VukaFormatOnSave : FileDocumentManagerListener {
    override fun beforeDocumentSaving(document: Document) {
        if (!VukaSettings.getInstance().state.formatOnSave) return
        val file = FileDocumentManager.getInstance().getFile(document) ?: return
        if (!isVukaFile(file)) return
        val project = ProjectLocator.getInstance().guessProjectForFile(file) ?: return
        val documents = PsiDocumentManager.getInstance(project)
        documents.commitDocument(document)
        val psi = documents.getPsiFile(document) ?: return
        WriteCommandAction.runWriteCommandAction(project, "Format Vuka File", null, {
            CodeStyleManager.getInstance(project).reformat(psi)
        })
    }
}
