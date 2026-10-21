package io.github.vukalang.vuka

import com.intellij.execution.process.CapturingProcessHandler
import com.intellij.notification.NotificationGroupManager
import com.intellij.notification.NotificationType
import com.intellij.openapi.diagnostic.logger
import com.intellij.openapi.project.Project
import com.intellij.openapi.project.ProjectLocator
import com.intellij.openapi.vfs.VfsUtil
import com.intellij.openapi.vfs.VfsUtilCore
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.openapi.vfs.newvfs.BulkFileListener
import com.intellij.openapi.vfs.newvfs.events.VFileContentChangeEvent
import com.intellij.openapi.vfs.newvfs.events.VFileEvent
import com.intellij.util.concurrency.AppExecutorUtil

// VukaGenOnSave writes the Go of each saved .vuka or .vuka.html file beside
// it (vuka gen -inplace on its package), when Settings | Languages &
// Frameworks | Vuka asks for it. GoLand reads Go only from disk, not
// through gopls, so this is what lets .go files see what .vuka files
// declare. The written files are .go, so they never start another run.
class VukaGenOnSave : BulkFileListener {
    override fun after(events: List<VFileEvent>) {
        if (!VukaSettings.getInstance().state.genOnSave) return
        val saved = events.filterIsInstance<VFileContentChangeEvent>().map { it.file }.filter(::isVukaFile)
        for ((project, files) in saved.groupBy { ProjectLocator.getInstance().guessProjectForFile(it) }) {
            if (project == null || project.isDisposed) continue
            val root = project.basePath?.let { VfsUtil.findFile(java.nio.file.Path.of(it), false) } ?: continue
            val dirs = files.mapNotNull { it.parent }.distinct().filter { VfsUtilCore.isAncestor(root, it, false) }
            if (dirs.isNotEmpty()) {
                queue.execute { gen(project, root, dirs) }
            }
        }
    }

    private fun gen(project: Project, root: VirtualFile, dirs: List<VirtualFile>) {
        if (project.isDisposed) return
        val packages = dirs.map { "./" + (VfsUtilCore.getRelativePath(it, root) ?: "") }
        try {
            val output = CapturingProcessHandler(vukaCommandLine(project, listOf("gen", "-inplace") + packages))
                .runProcess(120_000)
            if (output.exitCode != 0 || output.isTimeout) {
                notify(project, "vuka gen -inplace ${packages.joinToString(" ")} failed:\n${output.stderr.ifBlank { output.stdout }.trim()}")
            }
        } catch (e: Exception) {
            log.info(e)
        } finally {
            VfsUtil.markDirtyAndRefresh(true, false, true, *dirs.toTypedArray())
        }
    }

    private fun notify(project: Project, message: String) {
        NotificationGroupManager.getInstance().getNotificationGroup("Vuka")
            .createNotification("Vuka", message, NotificationType.WARNING)
            .notify(project)
    }

    companion object {
        private val log = logger<VukaGenOnSave>()

        // One gen at a time, in the order the files were saved.
        private val queue = AppExecutorUtil.createBoundedApplicationPoolExecutor("Vuka gen", 1)
    }
}
