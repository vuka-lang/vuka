package io.github.vukalang.vuka

import com.intellij.ide.FileIconProvider
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.IconLoader
import com.intellij.openapi.vfs.VirtualFile
import javax.swing.Icon

object VukaIcons {
    // The VS Code extension's file icon.
    @JvmField
    val FILE: Icon = IconLoader.getIcon("/icons/vuka.svg", VukaIcons::class.java)
}

// VukaIconProvider gives .vuka and .vuka.html files the Vuka icon, which
// their TextMate file type doesn't have.
class VukaIconProvider : FileIconProvider {
    override fun getIcon(file: VirtualFile, flags: Int, project: Project?): Icon? =
        if (file.name.endsWith(".vuka") || file.name.endsWith(".vuka.html")) VukaIcons.FILE else null
}
