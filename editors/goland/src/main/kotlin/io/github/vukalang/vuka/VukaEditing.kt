package io.github.vukalang.vuka

import com.intellij.codeInsight.editorActions.TypedHandlerDelegate
import com.intellij.codeInsight.editorActions.enter.EnterHandlerDelegate
import com.intellij.codeInsight.editorActions.enter.EnterHandlerDelegateAdapter
import com.intellij.codeInsight.template.TemplateActionContext
import com.intellij.codeInsight.template.TemplateContextType
import com.intellij.openapi.actionSystem.DataContext
import com.intellij.openapi.editor.Editor
import com.intellij.openapi.project.Project
import com.intellij.psi.PsiFile

private fun isVukaSource(file: PsiFile) = file.virtualFile?.name?.endsWith(".vuka") == true

private fun isBlank(c: Char) = c == ' ' || c == '\t'

// VukaTypedHandler finishes JSX tags as they are typed in .vuka files: an
// opening tag's > adds its closing tag after the caret, and </ the name of
// the element it closes (vuka.autoCloseTags in VS Code).
class VukaTypedHandler : TypedHandlerDelegate() {
    override fun charTyped(c: Char, project: Project, editor: Editor, file: PsiFile): Result {
        if ((c != '>' && c != '/') || !isVukaSource(file) || !VukaSettings.getInstance().state.autoCloseTags ||
            editor.caretModel.caretCount != 1
        ) {
            return Result.CONTINUE
        }
        val document = editor.document
        val offset = editor.caretModel.offset
        val text = document.charsSequence
        val before = text.subSequence(0, offset).toString()
        val after = text.subSequence(offset, document.getLineEndOffset(document.getLineNumber(offset))).toString()
        if (c == '/') {
            val closing = VukaJsx.closingFor(before, after) ?: return Result.CONTINUE
            document.insertString(offset, closing)
            editor.caretModel.moveToOffset(offset + closing.length)
        } else {
            val name = VukaJsx.tagToClose(before) ?: return Result.CONTINUE
            if (after.trimStart().startsWith("</$name>")) return Result.CONTINUE
            document.insertString(offset, "</$name>")
        }
        return Result.STOP
    }
}

// VukaEnterHandler indents after an opening tag in .vuka files, and puts a
// closing tag right after the caret on its own line: Enter in <td>|</td>
// leaves the caret indented between them, as the VS Code extension's
// onEnterRules do.
class VukaEnterHandler : EnterHandlerDelegateAdapter() {
    override fun postProcessEnter(file: PsiFile, editor: Editor, dataContext: DataContext): EnterHandlerDelegate.Result {
        if (!isVukaSource(file) || editor.caretModel.caretCount != 1) return EnterHandlerDelegate.Result.Continue
        val document = editor.document
        val caret = editor.caretModel.offset
        val line = document.getLineNumber(caret)
        if (line == 0) return EnterHandlerDelegate.Result.Continue
        val text = document.charsSequence
        val lineStart = document.getLineStartOffset(line)
        val prevStart = document.getLineStartOffset(line - 1)
        val prevEnd = document.getLineEndOffset(line - 1)
        if ((lineStart until caret).any { !isBlank(text[it]) } ||
            VukaJsx.tagToClose(text.subSequence(0, prevEnd).toString().trimEnd(' ', '\t')) == null
        ) {
            return EnterHandlerDelegate.Result.Continue
        }
        val indent = text.subSequence(prevStart, prevEnd).takeWhile(::isBlank).toString()
        val inner = indent + if (indent.startsWith(" ")) "    " else "\t"
        val rest = text.subSequence(caret, document.getLineEndOffset(line)).toString()
        val end = caret + rest.takeWhile(::isBlank).length
        document.replaceString(lineStart, end, if (rest.trimStart().startsWith("</")) "$inner\n$indent" else inner)
        editor.caretModel.moveToOffset(lineStart + inner.length)
        return EnterHandlerDelegate.Result.Continue
    }
}

// VukaTemplateContextType is the Vuka live template context: .vuka files,
// where the templates made from the VS Code extension's snippets apply.
class VukaTemplateContextType : TemplateContextType("Vuka") {
    override fun isInContext(templateActionContext: TemplateActionContext): Boolean =
        templateActionContext.file.name.endsWith(".vuka")
}
