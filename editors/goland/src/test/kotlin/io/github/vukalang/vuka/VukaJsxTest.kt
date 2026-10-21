package io.github.vukalang.vuka

import org.junit.Assert.assertEquals
import org.junit.Test
import java.io.File

// The tests of editors/vscode/test/jsx.test.mjs, on the same benchmark page,
// typed the way the editor would.
class VukaJsxTest {
    private val page = File("../vscode/test/fixtures/benchmark.vuka").readText()

    // upTo is the page up to the nth (1-based) occurrence of needle, and needle.
    private fun upTo(needle: String, n: Int = 1): String {
        var i = -1
        repeat(n) { i = page.indexOf(needle, i + 1) }
        check(i >= 0) { "no $needle" }
        return page.substring(0, i + needle.length)
    }

    // > closes the tag it finishes: first on a line, after another tag on the
    // line, in a block's body, a fragment; never a void, self-closing or Go <.
    @Test
    fun tagToClose() {
        for ((before, want) in listOf(
            upTo("return <>") to "",
            upTo("<h1>") to "h1",
            upTo("<table>") to "table",
            upTo("<tr>") to "tr",
            upTo("<tr><th>") to "th",
            upTo("</th><th>") to "th",
            upTo("pets {\n\t\t\t\t<tr>") to "tr",
            upTo("<td>") to "td",
            upTo("p.ID)}>") to "a",
            upTo("<td>", 3) to "td",
            "\treturn <input type=\"text\">" to null,
            "\treturn <br>" to null,
            "\treturn <Card />" to null,
            "\treturn <List[User] items={us}>" to "List",
            "\treturn <theme.Card>" to "theme.Card",
            "\treturn <b onClick={func() { if a > b { x() } }}>" to "b",
            "\treturn <p title=\"a > b\">" to "p",
            "\tif a < b && c >" to null,
            "\tx := y<b>" to null,
            "\treturn <p>{a >" to null,
            "\treturn <p title={a >" to null,
            "\t</td>" to null,
        )) {
            assertEquals("tagToClose(…${before.takeLast(30)})", want, VukaJsx.tagToClose(before))
        }
    }

    // </ completes the innermost element still open, through blocks.
    @Test
    fun closingFor() {
        for ((needle, want) in listOf(
            "<h1>Pets ({len(pets)})<" to "h1",
            "<th>Name<" to "th",
            "</th></tr>\n\t\t\t{for _, p := range pets {\n\t\t\t\t<tr>\n\t\t\t\t\t<td>\n\t\t\t\t\t\t<a href={fmt.Sprintf(\"/pets/%d\", p.ID)}>{p.Name}<" to "a",
            "{p.Name}</a>\n\t\t\t\t\t<" to "td",
            "<td>{p.Age}</td>\n\t\t\t\t<" to "tr",
            "}}\n\t\t<" to "table",
            "</table>\n\t<" to "",
        )) {
            val before = upTo(needle)
            assertEquals("closingFor(…${before.takeLast(25)}/)", "$want>", VukaJsx.closingFor("$before/", "\n"))
        }
        assertEquals("before an existing >", "p", VukaJsx.closingFor("\treturn <p>x</", ">"))
        assertEquals("before a name", null, VukaJsx.closingFor("\treturn <p>x</", "p>"))
        assertEquals("outside markup", null, VukaJsx.closingFor("\tx := 1 </", ""))
    }

    @Test
    fun unclosedTag() {
        assertEquals("skips Go strings", "i", VukaJsx.unclosedTag("\ts := \"<b>\"\n\treturn <i>"))
        assertEquals("none", null, VukaJsx.unclosedTag("\treturn <p>x</p>"))
    }
}
