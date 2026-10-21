import groovy.json.JsonSlurper
import org.gradle.api.DefaultTask
import org.gradle.api.file.DirectoryProperty
import org.gradle.api.file.RegularFileProperty
import org.gradle.api.tasks.InputFile
import org.gradle.api.tasks.OutputDirectory
import org.gradle.api.tasks.PathSensitive
import org.gradle.api.tasks.PathSensitivity
import org.gradle.api.tasks.TaskAction

// LiveTemplates turns the VS Code extension's snippets (snippets/vuka.json)
// into a live template set, liveTemplates/Vuka.xml, one template per prefix
// in the Vuka context. Tab stops become variables in their order, $0 the
// end, a choice an enum(…), and a /downcase or /upcase transform a variable
// that follows its tab stop.
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
        val out = StringBuilder("<templateSet group=\"Vuka\">\n")
        val seen = HashSet<String>()
        for ((title, snippet) in all) {
            val prefixes = when (val p = snippet["prefix"]) {
                is List<*> -> p.map { it.toString() }
                else -> listOf(p.toString())
            }
            val body = when (val b = snippet["body"]) {
                is List<*> -> b.joinToString("\n")
                else -> b.toString()
            }
            val description = snippet["description"]?.toString() ?: title
            val template = Snippet(body).template()
            // An abbreviation is what is typed before Tab: a word, once.
            for (prefix in prefixes.filter { Regex("\\w+").matches(it) && seen.add(it) }) {
                out.append("  <template name=\"").append(xml(prefix))
                    .append("\" value=\"").append(xml(template.text))
                    .append("\" description=\"").append(xml(description))
                    .append("\" toReformat=\"false\" toShortenFQNames=\"false\">\n")
                for (v in template.variables) {
                    out.append("    <variable name=\"").append(v.name)
                        .append("\" expression=\"").append(xml(v.expression))
                        .append("\" defaultValue=\"").append(xml(v.defaultValue))
                        .append("\" alwaysStopAt=\"").append(v.stop).append("\" />\n")
                }
                out.append("    <context>\n      <option name=\"VUKA\" value=\"true\" />\n    </context>\n  </template>\n")
            }
        }
        out.append("</templateSet>\n")
        val file = outputDir.get().file("liveTemplates/Vuka.xml").asFile
        file.parentFile.mkdirs()
        file.writeText(out.toString())
    }
}

private fun xml(s: String) = buildString {
    for (c in s) {
        when (c) {
            '&' -> append("&amp;")
            '<' -> append("&lt;")
            '>' -> append("&gt;")
            '"' -> append("&quot;")
            '\n' -> append("&#10;")
            '\t' -> append("&#9;")
            else -> append(c)
        }
    }
}

// quoted is s as a string in a live template expression.
private fun quoted(s: String) = "\"" + s.replace("\\", "\\\\").replace("\"", "\\\"") + "\""

class Variable(val name: String, val expression: String, val defaultValue: String, val stop: Boolean)

class Template(val text: String, val variables: List<Variable>)

// Snippet parses a VS Code snippet body: $N, ${N}, ${N:default}, ${N|a,b|},
// ${N/regex/format/} and \ escapes.
private class Snippet(val body: String) {
    private var i = 0
    private val stops = sortedMapOf<Int, Variable>()
    private val derived = ArrayList<Variable>()

    fun template(): Template {
        val text = until(null)
        val stops = stops.values.toList()
        return Template(text, stops + derived)
    }

    // until reads the template text up to the unescaped end, or the body's end.
    private fun until(end: Char?): String {
        val b = StringBuilder()
        while (i < body.length) {
            val c = body[i]
            when {
                c == '\\' && i + 1 < body.length && body[i + 1] in "\$}\\" -> {
                    b.append(if (body[i + 1] == '$') "$$" else body[i + 1].toString())
                    i += 2
                }
                end != null && c == end -> return b.toString()
                c == '$' -> b.append(dollar())
                else -> {
                    b.append(c)
                    i++
                }
            }
        }
        return b.toString()
    }

    private fun number(): Int {
        val start = i
        while (i < body.length && body[i].isDigit()) i++
        return body.substring(start, i).toInt()
    }

    // ref is the template text for tab stop n, declaring it if it's new.
    private fun ref(n: Int, default: String? = null, expression: String? = null): String {
        if (n == 0) return "\$END\$"
        val name = "V$n"
        if (default != null || expression != null || n !in stops) {
            val prev = stops[n]
            if (prev == null || prev.defaultValue == "\"\"") {
                val d = default?.let(::quoted) ?: "\"\""
                stops[n] = Variable(name, expression ?: d, d, true)
            }
        }
        return "\$$name\$"
    }

    // dollar reads what starts at a $.
    private fun dollar(): String {
        i++
        if (i < body.length && body[i].isDigit()) return ref(number())
        if (i + 1 < body.length && body[i] == '{' && body[i + 1].isDigit()) {
            i++
            val n = number()
            when (body.getOrNull(i)) {
                '}' -> {
                    i++
                    return ref(n)
                }
                ':' -> {
                    i++
                    // A placeholder's default is plain text in a live template.
                    val default = until('}').replace("$$", "$").replace(Regex("\\$[A-Za-z0-9_]+\\$"), "")
                    i++
                    return if (n == 0) "${default.replace("$", "$$")}\$END\$" else ref(n, default)
                }
                '|' -> {
                    i++
                    val close = body.indexOf("|}", i)
                    val choices = body.substring(i, close).split(',')
                    i = close + 2
                    return ref(n, choices.first(), "enum(" + choices.joinToString(",") { quoted(it) } + ")")
                }
                '/' -> {
                    val close = transformEnd()
                    val format = body.substring(i, close)
                    i = close + 1
                    val fn = when {
                        "/downcase" in format -> "lowercaseAndDash"
                        "/upcase" in format -> "capitalizeAndUnderscore"
                        else -> ""
                    }
                    val name = "V${n}_${derived.size}"
                    derived += Variable(name, if (fn.isEmpty()) "V$n" else "$fn(V$n)", "", false)
                    return "\$$name\$"
                }
            }
        }
        return "$$"
    }

    // transformEnd is the offset of the } ending the transform at i, past
    // the ${…} inside its format.
    private fun transformEnd(): Int {
        var depth = 0
        var j = i
        while (j < body.length) {
            when (body[j]) {
                '\\' -> j++
                '{' -> depth++
                '}' -> if (depth-- == 0) return j
            }
            j++
        }
        return body.length - 1
    }
}
