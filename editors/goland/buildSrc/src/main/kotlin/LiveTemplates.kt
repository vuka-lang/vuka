import groovy.json.JsonSlurper
import org.gradle.api.DefaultTask
import org.gradle.api.file.DirectoryProperty
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.tasks.InputFile
import org.gradle.api.tasks.OutputDirectory
import org.gradle.api.tasks.PathSensitive
import org.gradle.api.tasks.PathSensitivity
import org.gradle.api.tasks.TaskAction

// LiveTemplates writes the VS Code extension's snippets (snippets/vuka.json)
// as a live template set, liveTemplates/Vuka.xml.
abstract class LiveTemplates : DefaultTask() {
    @get:InputFile
    @get:PathSensitive(PathSensitivity.NONE)
    abstract val snippets: RegularFileProperty

    @get:OutputDirectory
    abstract val outputDir: DirectoryProperty

    @TaskAction
    fun generate() {
        @Suppress("UNCHECKED_CAST")
        val all = JsonSlurper().parse(snippets.get().asFile) as Map<String, Map<String, Any>>
        val file = outputDir.get().file("liveTemplates/Vuka.xml").asFile
        file.parentFile.mkdirs()
        file.writeText(liveTemplates(all))
    }
}

