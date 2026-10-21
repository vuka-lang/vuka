package io.github.vukalang.vuka

import com.goide.psi.GoArgumentList
import com.goide.psi.GoCallExpr
import com.goide.psi.GoReferenceExpression
import com.goide.psi.GoStringLiteral
import com.intellij.lang.Language
import com.intellij.lang.injection.MultiHostInjector
import com.intellij.lang.injection.MultiHostRegistrar
import com.intellij.openapi.util.TextRange
import com.intellij.psi.PsiElement

// VukaGoHtmlInjector reads the raw string of a ui.HTML(`…`) call in a .go
// file as HTML, as the VS Code extension's inline.vuka-html grammar does:
// highlighting, completion and tag matching in the markup. Loaded only with
// the Go plugin (vuka-go.xml).
class VukaGoHtmlInjector : MultiHostInjector {
    override fun elementsToInjectIn(): List<Class<out PsiElement>> = listOf(GoStringLiteral::class.java)

    override fun getLanguagesToInject(registrar: MultiHostRegistrar, context: PsiElement) {
        val literal = context as? GoStringLiteral ?: return
        val text = literal.text
        if (text.length < 2 || !text.startsWith("`") || !text.endsWith("`")) return
        val args = literal.parent as? GoArgumentList ?: return
        val call = args.parent as? GoCallExpr ?: return
        if (args.expressionList.firstOrNull() != literal) return
        if ((call.expression as? GoReferenceExpression)?.identifier?.text != "HTML") return
        val html = Language.findLanguageByID("HTML") ?: return
        registrar.startInjecting(html)
            .addPlace(null, null, literal, TextRange(1, text.length - 1))
            .doneInjecting()
    }
}
