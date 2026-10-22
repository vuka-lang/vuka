package main

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// componentFiles are a page using the package's components: struct
// components in .vuka.html files, a live one, and a templ component.
var componentFiles = map[string]string{
	"main.go":     "package main\n\ntype User struct {\n\tName string\n\tYear int\n}\n\nfunc main() {}\n",
	"logic.vuka":  "package main\n\nfunc less(a, b int) bool { return a < b }\n",
	"hello.templ": "package main\n\ntempl Hello(name string) {\n\t<p>Hello, { name }!</p>\n}\n",
	"button.vuka.html": `package main

import "github.com/vuka-lang/ui"

type Button struct {
	Variant  string
	Disabled bool
	Children ui.Node
}
<button>{Variant}</button>
`,
	"usercard.vuka.html": `package main

import "github.com/vuka-lang/ui"

type UserCard struct {
	User     User
	Admin    bool
	Children ui.Node
}
<div class="card">
	<h3>{User.Name}</h3>
	{Children}
</div>
`,
	"counter.vuka.html": `package main

import "github.com/vuka-lang/ui"

type Counter struct {
	ui.Live
	Start int
	Label string
	n     int
}

func (c *Counter) Inc() { c.n++ }
<b onClick={Inc}>{n}</b>
`,
	"page.vuka.html": componentPage,
}

const componentPage = `package main

type Page struct{ Users []User }
<section>
	for _, u := range Users {
		<UserCard user={u} admin>since {u.Year}</UserCard>
	}
	<Button variant="primary">Go</Button>
	<Hello name="vuka" />
	<Counter start={1} />
</section>
`

// TestLSPComponents: in a .vuka.html page, a component's tags, opening and
// closing, and its attributes are its struct and fields (a templ
// component's, its func and parameters) for hover, definition, references,
// rename and completion; and the page keeps working while being typed.
func TestLSPComponents(t *testing.T) {
	c, dir, init := startLSPWith(t, templFiles(t, componentFiles))
	caps := init.(map[string]any)["capabilities"].(map[string]any)
	if b, _ := json.Marshal(caps["completionProvider"]); !strings.Contains(string(b), `"\`+`u003c"`) {
		t.Fatalf("< isn't a completion trigger: %s", b)
	}
	src0 := componentPage
	uri := pathToURI(filepath.Join(dir, "page.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": src0}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })

	version := 1
	edit := func(src string) {
		version++
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": version},
			"contentChanges": []any{map[string]any{"text": src}}})
	}
	at := func(src, needle string, delta int) map[string]any {
		i := strings.Index(src, needle)
		if i < 0 {
			t.Fatalf("no %q", needle)
		}
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(src), i+delta)}
	}
	ask := func(method string, params map[string]any) string {
		b, _ := json.Marshal(c.call(method, params))
		return string(b)
	}
	// posOf is the position of needle (+delta) in src, as JSON.
	posOf := func(src, needle string, delta int) string {
		pos := positionOf([]byte(src), strings.Index(src, needle)+delta)
		b, _ := json.Marshal(map[string]any{"line": pos.Line, "character": pos.Character})
		return string(b)
	}
	labels := func(v string) []string {
		var r struct {
			Items []struct {
				Label    string `json:"label"`
				TextEdit struct {
					NewText string `json:"newText"`
				} `json:"textEdit"`
			} `json:"items"`
		}
		_ = json.Unmarshal([]byte(v), &r)
		var out []string
		for _, it := range r.Items {
			out = append(out, it.Label+"="+it.TextEdit.NewText)
		}
		sort.Strings(out)
		return out
	}
	has := func(list []string, want string) bool {
		for _, l := range list {
			if l == want {
				return true
			}
		}
		return false
	}
	card := componentFiles["usercard.vuka.html"]

	t.Run("tag names", func(t *testing.T) {
		decl := posOf(card, "UserCard struct", 0)
		for _, needle := range []string{"<UserCard user", "</UserCard>"} {
			delta := strings.Index(needle, "U") + 2
			if h := ask("textDocument/hover", at(src0, needle, delta)); !strings.Contains(h, "type UserCard struct") {
				t.Fatalf("hover on %s: %s", needle, h)
			}
			if d := ask("textDocument/definition", at(src0, needle, delta)); !strings.Contains(d, "usercard.vuka.html") || !strings.Contains(d, `"start":`+decl) {
				t.Fatalf("definition from %s: %s, want %s", needle, d, decl)
			}
		}
		if h := ask("textDocument/hover", at(src0, "</UserCard>", 4)); !strings.Contains(h, `"range":{"end":`+posOf(src0, "</UserCard>", 10)) {
			t.Fatalf("hover on the closing tag should cover it: %s", h)
		}
		if h := ask("textDocument/hover", at(src0, "<Hello", 2)); !strings.Contains(h, "func Hello(name string) templ.Component") {
			t.Fatalf("hover on a templ component's tag: %s", h)
		}
	})

	t.Run("attribute names", func(t *testing.T) {
		if h := ask("textDocument/hover", at(src0, "user={u}", 1)); !strings.Contains(h, "field User User") {
			t.Fatalf("hover on user=: %s", h)
		}
		if d := ask("textDocument/definition", at(src0, "user={u}", 1)); !strings.Contains(d, `"start":`+posOf(card, "User     User", 0)) {
			t.Fatalf("definition of user=: %s", d)
		}
		if h := ask("textDocument/hover", at(src0, "variant=", 1)); !strings.Contains(h, "field Variant string") {
			t.Fatalf("hover on variant=: %s", h)
		}
		if d := ask("textDocument/definition", at(src0, "variant=", 1)); !strings.Contains(d, "button.vuka.html") || !strings.Contains(d, `"start":`+posOf(componentFiles["button.vuka.html"], "Variant  string", 0)) {
			t.Fatalf("definition of variant=: %s", d)
		}
		if d := ask("textDocument/definition", at(src0, "start={1}", 1)); !strings.Contains(d, "counter.vuka.html") || !strings.Contains(d, `"start":`+posOf(componentFiles["counter.vuka.html"], "Start int", 0)) {
			t.Fatalf("definition of a live component's start=: %s", d)
		}
		if d := ask("textDocument/definition", at(src0, `name="vuka"`, 1)); !strings.Contains(d, "hello.templ") {
			t.Fatalf("definition of a templ component's name=: %s", d)
		}
	})

	t.Run("references and rename cover closing tags", func(t *testing.T) {
		close := `"start":` + posOf(src0, "</UserCard>", 2)
		if r := ask("textDocument/references", at(src0, "<UserCard user", 2)); !strings.Contains(r, close) {
			t.Fatalf("references: %s", r)
		}
		for _, from := range []map[string]any{at(src0, "<UserCard user", 2), at(src0, "</UserCard>", 4)} {
			from["newName"] = "Card"
			// The two tags and the struct's name, nothing Vuka wrote.
			r := ask("textDocument/rename", from)
			if strings.Count(r, `"newText":"Card"`) != 3 || !strings.Contains(r, close) || !strings.Contains(r, `"start":`+posOf(card, "UserCard struct", 0)) {
				t.Fatalf("rename: %s", r)
			}
		}
		if r := ask("textDocument/prepareRename", at(src0, "</UserCard>", 4)); !strings.Contains(r, close) {
			t.Fatalf("prepareRename on a closing tag: %s", r)
		}
	})

	complete := func(src, needle string, delta int) []string {
		edit(src)
		return labels(ask("textDocument/completion", at(src, needle, delta)))
	}

	t.Run("completing a tag", func(t *testing.T) {
		src := strings.Replace(src0, "\t<Button variant", "\t<Us\n\t<Button variant", 1)
		got := complete(src, "<Us\n", 3)
		if !has(got, "UserCard=UserCard") || !has(got, "div=div") || has(got, "User=User") {
			t.Fatalf("<Us: %v", got)
		}
		src = strings.Replace(src0, "\t<Button variant", "\t<Cou\n\t<Button variant", 1)
		if got := complete(src, "<Cou\n", 4); !has(got, "Counter=Counter") {
			t.Fatalf("<Cou: a live component: %v", got)
		}
		src = strings.Replace(src0, "\t<Button variant", "\t<Hel\n\t<Button variant", 1)
		if got := complete(src, "<Hel\n", 4); !has(got, "Hello=Hello") {
			t.Fatalf("<Hel: a templ component: %v", got)
		}
		// The tag being typed doesn't stop the rest of the file working.
		if h := ask("textDocument/hover", at(src, "u.Year", 0)); !strings.Contains(h, "var u User") {
			t.Fatalf("hover elsewhere mid-tag: %s", h)
		}
	})

	t.Run("completing attributes", func(t *testing.T) {
		src := strings.Replace(src0, "\t<Button variant", "\t<UserCard \n\t<Button variant", 1)
		if got := complete(src, "<UserCard \n", 10); !has(got, "admin=admin") || !has(got, "user=user={$1}") {
			t.Fatalf("<UserCard : %v", got)
		}
		src = strings.Replace(src0, "\t<Button variant", "\t<UserCard user={Users[0]} a\n\t<Button variant", 1)
		if got := complete(src, "} a\n", 3); !has(got, "admin=admin") || has(got, "user=user={$1}") {
			t.Fatalf("after user=: %v", got)
		}
		src = strings.Replace(src0, `<Button variant="primary">`, `<Button variant="primary" >`, 1)
		if got := complete(src, `"primary" >`, 10); !has(got, "disabled=disabled") || has(got, `variant=variant="$1"`) {
			t.Fatalf("<Button: %v", got)
		}
		src = strings.Replace(src0, `<Counter start={1} />`, `<Counter start={1}  />`, 1)
		if got := complete(src, `<Counter start={1}  />`, 19); strings.Join(got, " ") != `Label=Label="$1"` {
			t.Fatalf("<Counter: %v", got)
		}
		src = strings.Replace(src0, `<Hello name="vuka" />`, `<Hello />`, 1)
		if got := complete(src, `<Hello />`, 7); strings.Join(got, " ") != `name=name="$1"` {
			t.Fatalf("<Hello: %v", got)
		}
	})

	t.Run("completing a closing tag", func(t *testing.T) {
		src := strings.Replace(src0, "since {u.Year}</UserCard>", "since {u.Year}</", 1)
		if got := complete(src, "since {u.Year}</", 16); strings.Join(got, " ") != "UserCard=UserCard>" {
			t.Fatalf("</: %v", got)
		}
	})

	t.Run("completing in braces", func(t *testing.T) {
		src := strings.Replace(src0, "since {u.Year}", "since {u.}", 1)
		if got := complete(src, "{u.}", 3); !has(got, "Name=Name") || !has(got, "Year=Year") {
			t.Fatalf("{u.}: %v", got)
		}
	})

	t.Run("markup triggers don't complete a logic file's Go", func(t *testing.T) {
		luri := pathToURI(filepath.Join(dir, "logic.vuka"))
		logic := componentFiles["logic.vuka"]
		c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
			"uri": luri, "languageId": "vuka", "version": 1, "text": logic}})
		c.waitDiags(luri, func(ds []any) bool { return len(ds) == 0 })
		p := map[string]any{"textDocument": map[string]any{"uri": luri}, "position": positionOf([]byte(logic), strings.Index(logic, "a < b")+3)}
		p["context"] = map[string]any{"triggerKind": 2, "triggerCharacter": "<"}
		if got := labels(ask("textDocument/completion", p)); len(got) != 0 {
			t.Fatalf("a <: %v", got)
		}
	})

	t.Run("edits apart while a tag is unclosed", func(t *testing.T) {
		src := strings.Replace(src0, "type Page struct{ Users []User }", "type Page struct {\n\tTitle string\n\tUsers []User\n}", 1)
		src = strings.Replace(src, "<Button variant=\"primary\">Go</Button>", "<Button variant=\"primary\">Go\n", 1)
		edit(src)
		c.waitDiags(uri, func(ds []any) bool {
			b, _ := json.Marshal(ds)
			return strings.Contains(string(b), "Button")
		})
		if h := ask("textDocument/hover", at(src, "u.Year", 0)); !strings.Contains(h, "var u User") || !strings.Contains(h, posOf(src, "u.Year", 0)) {
			t.Fatalf("hover between the edits: %s", h)
		}
		if d := ask("textDocument/definition", at(src, "<UserCard user", 3)); !strings.Contains(d, "usercard.vuka.html") {
			t.Fatalf("definition between the edits: %s", d)
		}
		if h := ask("textDocument/hover", at(src, "start={1}", 1)); !strings.Contains(h, "Start int") {
			t.Fatalf("hover after the edits: %s", h)
		}
	})

	t.Run("an unclosed tag", func(t *testing.T) {
		src := strings.Replace(src0, "since {u.Year}</UserCard>", "since {u.Year}\n", 1)
		edit(src)
		ds := c.waitDiags(uri, func(ds []any) bool {
			b, _ := json.Marshal(ds)
			return strings.Contains(string(b), "UserCard")
		})
		b, _ := json.Marshal(ds)
		if !strings.Contains(string(b), "at line 6") || !strings.Contains(string(b), `"start":`+posOf(src, "</section>", 0)) {
			t.Fatalf("diagnostics: %s", b)
		}
		// The rest of the file, lines below the edit included, still works.
		if h := ask("textDocument/hover", at(src, "start={1}", 1)); !strings.Contains(h, "Start int") {
			t.Fatalf("hover below the unclosed tag: %s", h)
		}
	})

	t.Run("nested blocks", func(t *testing.T) {
		src := strings.Replace(src0, "</UserCard>\n", "</UserCard>\n\t\tif u.Year > 2000 {\n\t\t\t<i>{u.Name}</i>\n\t\t}\n", 1)
		edit(src)
		c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
		if h := ask("textDocument/hover", at(src, "u.Year > 2000", 0)); !strings.Contains(h, "var u User") || !strings.Contains(h, posOf(src, "u.Year > 2000", 0)) {
			t.Fatalf("hover in a nested header: %s", h)
		}
		if h := ask("textDocument/hover", at(src, "u.Year > 2000", 3)); !strings.Contains(h, "Year int") {
			t.Fatalf("hover on a field in a nested header: %s", h)
		}
		src = strings.Replace(src, "if u.Year > 2000", "if u.Ye > 2000", 1)
		if got := complete(src, "u.Ye > 2000", 4); !has(got, "Year=Year") {
			t.Fatalf("completing in a nested header: %v", got)
		}
	})
}

func TestCompletable(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"\treturn <div>\n\t\t<UserCard user={u} ad\n\t</div>", "\treturn <div>\n\t\t               {u}   \n\t</div>"},
		{"\treturn <section>\n\t\t<\n\t</section>", "\treturn <section>\n\t\t \n\t</section>"},
		{"\t{user.}", "\t{user." + placeholder + "}"},
		{"\tfmt.\n\tx := 1.\n\tf(xs...)\n\t// see fmt.\n", "\tfmt." + placeholder + "\n\tx := 1.\n\tf(xs...)\n\t// see fmt.\n"},
		{"\tif a <b {\n\t}\n\tv := <-ch\n\treturn <a href={u}>x</a>", "\tif a <b {\n\t}\n\tv := <-ch\n\treturn <a href={u}>x</a>"},
	} {
		out, hunks := completable([]byte(tc.in))
		if string(out) != tc.want {
			t.Errorf("completable(%q) = %q, want %q", tc.in, out, tc.want)
		}
		f := &vfile{from: out}
		f.withText([]byte(tc.in), hunks)
		if i := strings.Index(tc.in, "user."); i >= 0 {
			if got := f.fromOff(i + 5); got != i+5 {
				t.Errorf("cursor after the dot maps to %d", got)
			}
			if got := f.fromOff(i + 6); got != i+6+len(placeholder) {
				t.Errorf("after the placeholder maps to %d", got)
			}
			if got := f.curOff(i + 6 + len(placeholder)); got != i+6 {
				t.Errorf("back from after the placeholder: %d", got)
			}
		}
	}
}
