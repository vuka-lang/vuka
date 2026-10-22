package io.github.vukalang.vuka

import java.util.regex.Pattern

// VukaJsx is the plugin's reading of JSX in .vuka text, apart from the IDE so
// it can be tested alone: which tag a typed > finishes, and which element a
// typed </ closes. It is editors/vscode/jsx.js, line for line.
object VukaJsx {
    val voidElements = setOf("area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source", "track", "wbr")

    private val nameAt = Pattern.compile("[A-Za-z_][\\w.:-]*")
    private val wordOrCloser = Regex("[\\w)\\]]")
    private val endsInReturn = Regex("return$")
    private val trailingBlanks = Regex("[ \\t]+$")
    private val takesOperand = Regex("(?:[\\n(,={:\\[;>}]|\\breturn)$")
    private val nameStart = Regex("^[\\w.:-]")

    data class Tag(val name: String, val close: Boolean, val self: Boolean, val end: Int)

    private fun isSpace(c: Char) = c == ' ' || c == '\t' || c == '\n' || c == '\r'

    // nameAt is the tag or attribute name starting at i, "" when none does.
    private fun nameAt(s: String, i: Int): String {
        if (i >= s.length) return ""
        val m = nameAt.matcher(s).region(i, minOf(s.length, i + 200))
        return if (m.lookingAt()) m.group() else ""
    }

    // skipQuoted is the offset after the string at i, -1 when it isn't closed.
    private fun skipQuoted(s: String, i: Int): Int {
        val q = s[i]
        var j = i + 1
        while (j < s.length) {
            if (s[j] == '\\' && q != '`') j++
            else if (s[j] == q) return j + 1
            else if (s[j] == '\n' && q != '`') return -1
            j++
        }
        return -1
    }

    // skipBraces is the offset after the } closing the { at i, -1 when none does.
    private fun skipBraces(s: String, i: Int): Int {
        var depth = 0
        var j = i
        while (j < s.length) {
            val c = s[j]
            if (c == '{') depth++
            else if (c == '}' && --depth == 0) return j + 1
            else if (c == '"' || c == '\'' || c == '`') {
                val e = skipQuoted(s, j)
                if (e < 0) return -1
                j = e - 1
            }
            j++
        }
        return -1
    }

    // readTag reads a whole tag at the < at i, or null when there is none, or
    // not all of it yet. A fragment is <> or </>.
    fun readTag(s: String, i: Int): Tag? {
        var j = i + 1
        val close = s.getOrNull(j) == '/'
        if (close) j++
        val name = nameAt(s, j)
        j += name.length
        if (name.isNotEmpty() && !close && s.getOrNull(j) == '[') {
            val e = s.indexOf(']', j)
            if (e < 0 || s.substring(j, e).any { it == '\n' || it == '<' || it == '>' }) return null
            j = e + 1
        }
        if (name.isEmpty() && s.getOrNull(j) != '>') return null
        while (true) {
            while (j < s.length && isSpace(s[j])) j++
            if (j >= s.length) return null
            val c = s[j]
            if (c == '>') return Tag(name, close, false, j + 1)
            if (c == '/' && s.getOrNull(j + 1) == '>' && !close) return Tag(name, close, true, j + 2)
            if (close) return null
            if (c == '{') {
                j = skipBraces(s, j)
                if (j < 0) return null
                continue
            }
            val a = nameAt(s, j)
            if (a.isEmpty()) return null
            j += a.length
            var k = j
            while (k < s.length && isSpace(s[k])) k++
            if (s.getOrNull(k) != '=') continue
            k++
            while (k < s.length && isSpace(s[k])) k++
            j = when (s.getOrNull(k)) {
                '"', '\'' -> skipQuoted(s, k)
                '{' -> skipBraces(s, k)
                else -> return null
            }
            if (j < 0) return null
        }
    }

    // opensMarkup reports whether a < at i can start markup in Go: after what
    // takes an operand (return ( , = { : [ ;), after a tag or a {…} child, or
    // first on its line. Inside markup any tag-shaped < is one.
    private fun opensMarkup(s: String, i: Int, inMarkup: Boolean): Boolean {
        val before = s.getOrNull(i - 1)
        if (before != null && wordOrCloser.matches(before.toString()) && !endsInReturn.containsMatchIn(s.substring(0, i))) return false
        if (inMarkup) return true
        val prev = s.substring(maxOf(0, i - 200), i).replace(trailingBlanks, "")
        return prev.isEmpty() || takesOperand.containsMatchIn(prev)
    }

    // tagToClose is the name of the opening tag a > typed at the end of before
    // finishes ("" for a fragment), or null: not a tag, a self-closing or void one.
    fun tagToClose(before: String): String? {
        if (!before.endsWith(">")) return null
        val floor = maxOf(0, before.length - 4000)
        var i = before.lastIndexOf('<')
        var n = 0
        while (i >= floor && n < 64) {
            val t = readTag(before, i)
            if (t != null && t.end == before.length) {
                if (t.close || t.self || t.name in voidElements || !opensMarkup(before, i, t.name != "")) return null
                return t.name
            }
            if (i == 0) break
            i = before.lastIndexOf('<', i - 1)
            n++
        }
        return null
    }

    // unclosedTag is the innermost element still open at the end of before, ""
    // for a fragment, null when none is.
    fun unclosedTag(before: String): String? {
        val s = if (before.length > 200000) before.takeLast(200000) else before
        val stack = ArrayList<String>()
        var i = 0
        while (i < s.length) {
            val c = s[i]
            if (stack.isEmpty() && (c == '"' || c == '`' || c == '\'')) {
                val e = skipQuoted(s, i)
                if (e > 0) i = e - 1
                i++
                continue
            }
            if (stack.isEmpty() && c == '/' && s.getOrNull(i + 1) == '/') {
                val e = s.indexOf('\n', i)
                i = if (e < 0) s.length else e
                i++
                continue
            }
            if (c != '<') {
                i++
                continue
            }
            val t = readTag(s, i)
            if (t == null) {
                i++
                continue
            }
            if (t.close) {
                val k = stack.lastIndexOf(t.name)
                if (k >= 0) stack.subList(k, stack.size).clear()
            } else if (opensMarkup(s, i, stack.isNotEmpty())) {
                if (!t.self && t.name !in voidElements) stack.add(t.name)
            } else {
                i++
                continue
            }
            i = t.end
        }
        return stack.lastOrNull()
    }

    // closingFor is what typing / after < should add: the open element's name
    // and its >, or null. after is the text after the cursor.
    fun closingFor(before: String, after: String): String? {
        if (!before.endsWith("</") || nameStart.containsMatchIn(after)) return null
        val name = unclosedTag(before.dropLast(2)) ?: return null
        if (name == "" && after.startsWith(">")) return null
        return if (after.startsWith(">")) name else "$name>"
    }
}
