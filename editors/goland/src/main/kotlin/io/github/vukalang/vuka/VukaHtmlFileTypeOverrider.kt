package io.github.vukalang.vuka

import com.intellij.openapi.fileTypes.FileType
import com.intellij.openapi.fileTypes.impl.FileTypeOverrider
import com.intellij.openapi.vfs.VirtualFile
import org.jetbrains.plugins.textmate.TextMateFileType

// VukaHtmlFileTypeOverrider opens .vuka.html files with the Vuka grammar, as
// VS Code does (Go in braces and at line starts, TypeScript in
// <script lang="ts">, CSS in <style>), instead of as HTML, whose file type
// their .html claims. Settings | Languages & Frameworks | Vuka turns it off.
class VukaHtmlFileTypeOverrider : FileTypeOverrider {
    override fun getOverriddenFileType(file: VirtualFile): FileType? =
        if (file.name.endsWith(".vuka.html") && VukaSettings.getInstance().state.templateGrammar) TextMateFileType.INSTANCE else null
}
