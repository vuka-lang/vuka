package io.github.vukalang.vuka

import com.intellij.execution.ExecutionException
import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.execution.configurations.PathEnvironmentVariableUtil
import com.intellij.notification.NotificationGroupManager
import com.intellij.notification.NotificationType
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.SystemInfo
import com.intellij.util.EnvironmentUtil
import java.io.File
import java.nio.charset.StandardCharsets

private const val INSTALL = "go install github.com/vuka-lang/vuka/cmd/vuka@latest"

// The tool directive go get -tool writes, in a single line or a tool block.
private val VUKA_TOOL = Regex("""(?m)^\s*(tool\s+)?github\.com/vuka-lang/vuka/cmd/vuka\s*$""")

// vukaLspCommandLine is the command running vuka lsp for project, the first of:
//   1. the vuka binary set in Settings | Languages & Frameworks | Vuka,
//   2. go tool vuka, when the project's go.mod declares vuka as a tool, so
//      the server is the version the project builds with,
//   3. vuka on PATH, then in $GOBIN, $GOPATH/bin and ~/go/bin.
fun vukaLspCommandLine(project: Project): GeneralCommandLine {
    val settings = VukaSettings.getInstance().state
    val args = mutableListOf("lsp")
    settings.goplsPath?.takeIf { it.isNotBlank() }?.let { args += listOf("-gopls", it) }
    if (!settings.sharedGopls) {
        args += "-shared=false"
    }
    val root = project.basePath
    val configured = settings.vukaPath?.takeIf { it.isNotBlank() }
    val command = when {
        configured != null -> GeneralCommandLine(configured)
        root != null && declaresVukaTool(File(root)) ->
            GeneralCommandLine(findTool("go") ?: "go", "tool", "vuka")
        else -> findTool("vuka")?.let { GeneralCommandLine(it) } ?: notFound(project)
    }
    return command
        .withParameters(args)
        .withWorkDirectory(root)
        .withParentEnvironmentType(GeneralCommandLine.ParentEnvironmentType.CONSOLE)
        .withCharset(StandardCharsets.UTF_8)
}

private fun declaresVukaTool(root: File): Boolean {
    val mod = File(root, "go.mod")
    return mod.isFile && VUKA_TOOL.containsMatchIn(mod.readText())
}

// findTool is a command's absolute path: on PATH (the login shell's, which
// the IDE loads when started from the Dock), else in $GOBIN, $GOPATH/bin or
// ~/go/bin; null when it's nowhere.
fun findTool(name: String): String? {
    PathEnvironmentVariableUtil.findExecutableInPathOnAnyOS(name)?.let { return it.absolutePath }
    val exe = if (SystemInfo.isWindows) "$name.exe" else name
    val home = System.getProperty("user.home")
    val gopath = EnvironmentUtil.getValue("GOPATH")?.split(File.pathSeparatorChar)?.firstOrNull { it.isNotBlank() }
        ?: File(home, "go").path
    return listOfNotNull(EnvironmentUtil.getValue("GOBIN"), File(gopath, "bin").path, File(home, "go/bin").path)
        .map { File(it, exe) }
        .firstOrNull { it.canExecute() }
        ?.absolutePath
}

private fun notFound(project: Project): Nothing {
    val message = "Couldn't find vuka. Install it with: $INSTALL, add it to the module with " +
        "go get -tool github.com/vuka-lang/vuka/cmd/vuka, or set its path in Settings | Languages & Frameworks | Vuka."
    NotificationGroupManager.getInstance().getNotificationGroup("Vuka")
        .createNotification("Vuka", message, NotificationType.ERROR)
        .notify(project)
    throw ExecutionException(message)
}
