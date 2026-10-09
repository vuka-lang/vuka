package main

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const jsxSource = `package main

type User struct {
	Name string
	Year int
}

type ButtonProps struct {
	Variant  string
	Disabled bool
	Children vuka.Node
}

func Button(props ButtonProps) vuka.Node { return <button>{props.Variant}</button> }

func UserCard(user User, admin bool, children vuka.Node) vuka.Node {
	return <div className="card">
		<h3>{user.Name}</h3>
		{children}
	</div>
}

func Page(users []User) vuka.Node {
	return <section>
		{for _, u := range users {
			<UserCard user={u} admin>since {u.Year}</UserCard>
		}}
		<Button variant="primary">Go</Button>
		<Hello name="vuka" />
	</section>
}

func main() {}
`

const jsxTempl = `package main

templ Hello(name string) {
	<p>Hello, { name }!</p>
}
`

func TestLSPJSX(t *testing.T) {
	c, dir, init := startLSPWith(t, templFiles(t, map[string]string{"main.vuka": jsxSource, "hello.templ": jsxTempl}))
	caps := init.(map[string]any)["capabilities"].(map[string]any)
	if b, _ := json.Marshal(caps["completionProvider"]); !strings.Contains(string(b), `"\`+`u003c"`) {
		t.Fatalf("< isn't a completion trigger: %s", b)
	}
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": jsxSource}})
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

	t.Run("tag names", func(t *testing.T) {
		funcDecl := posOf(jsxSource, "UserCard(user User", 0)
		for _, needle := range []string{"<UserCard user", "</UserCard>"} {
			delta := strings.Index(needle, "U") + 2
			if h := ask("textDocument/hover", at(jsxSource, needle, delta)); !strings.Contains(h, "func UserCard(user User, admin bool, children vuka.Node) vuka.Node") {
				t.Fatalf("hover on %s: %s", needle, h)
			}
			if d := ask("textDocument/definition", at(jsxSource, needle, delta)); !strings.Contains(d, `"start":`+funcDecl) {
				t.Fatalf("definition from %s: %s, want %s", needle, d, funcDecl)
			}
		}
		if h := ask("textDocument/hover", at(jsxSource, "</UserCard>", 4)); !strings.Contains(h, `"range":{"end":`+posOf(jsxSource, "</UserCard>", 10)) {
			t.Fatalf("hover on the closing tag should cover it: %s", h)
		}
	})

	t.Run("attribute names", func(t *testing.T) {
		if h := ask("textDocument/hover", at(jsxSource, "user={u}", 1)); !strings.Contains(h, "var user User") {
			t.Fatalf("hover on user=: %s", h)
		}
		if d := ask("textDocument/definition", at(jsxSource, "user={u}", 1)); !strings.Contains(d, `"start":`+posOf(jsxSource, "user User, admin", 0)) {
			t.Fatalf("definition of user=: %s", d)
		}
		if h := ask("textDocument/hover", at(jsxSource, "variant=", 1)); !strings.Contains(h, "field Variant string") {
			t.Fatalf("hover on variant=: %s", h)
		}
		if d := ask("textDocument/definition", at(jsxSource, "variant=", 1)); !strings.Contains(d, `"start":`+posOf(jsxSource, "Variant  string", 0)) {
			t.Fatalf("definition of variant=: %s", d)
		}
		if d := ask("textDocument/definition", at(jsxSource, `name="vuka"`, 1)); !strings.Contains(d, "hello.templ") {
			t.Fatalf("definition of a templ component's name=: %s", d)
		}
	})

	t.Run("references and rename cover closing tags", func(t *testing.T) {
		close := `"start":` + posOf(jsxSource, "</UserCard>", 2)
		if r := ask("textDocument/references", at(jsxSource, "func UserCard", 6)); !strings.Contains(r, close) {
			t.Fatalf("references: %s", r)
		}
		for _, from := range []map[string]any{at(jsxSource, "<UserCard user", 2), at(jsxSource, "</UserCard>", 4)} {
			from["newName"] = "Card"
			r := ask("textDocument/rename", from)
			if strings.Count(r, `"newText":"Card"`) != 3 || !strings.Contains(r, close) {
				t.Fatalf("rename: %s", r)
			}
		}
		if r := ask("textDocument/prepareRename", at(jsxSource, "</UserCard>", 4)); !strings.Contains(r, close) {
			t.Fatalf("prepareRename on a closing tag: %s", r)
		}
	})

	complete := func(src, needle string, delta int) []string {
		edit(src)
		return labels(ask("textDocument/completion", at(src, needle, delta)))
	}

	t.Run("completing a tag", func(t *testing.T) {
		src := strings.Replace(jsxSource, "\t\t<Button variant", "\t\t<Us\n\t\t<Button variant", 1)
		got := complete(src, "<Us\n", 3)
		if !has(got, "UserCard=UserCard") || !has(got, "div=div") || has(got, "User=User") {
			t.Fatalf("<Us: %v", got)
		}
		src = strings.Replace(jsxSource, "\t\t<Button variant", "\t\t<Hel\n\t\t<Button variant", 1)
		if got := complete(src, "<Hel\n", 4); !has(got, "Hello=Hello") {
			t.Fatalf("<Hel: a templ component: %v", got)
		}
		// The tag being typed doesn't stop the rest of the file working.
		c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
		if h := ask("textDocument/hover", at(src, "u.Year", 0)); !strings.Contains(h, "var u User") {
			t.Fatalf("hover elsewhere mid-tag: %s", h)
		}
	})

	t.Run("completing attributes", func(t *testing.T) {
		src := strings.Replace(jsxSource, "\t\t<Button variant", "\t\t<UserCard \n\t\t<Button variant", 1)
		if got := complete(src, "<UserCard \n", 10); strings.Join(got, " ") != "admin=admin user=user={$1}" {
			t.Fatalf("<UserCard : %v", got)
		}
		src = strings.Replace(jsxSource, "\t\t<Button variant", "\t\t<UserCard user={users[0]} a\n\t\t<Button variant", 1)
		if got := complete(src, "} a\n", 3); strings.Join(got, " ") != "admin=admin" {
			t.Fatalf("after user=: %v", got)
		}
		src = strings.Replace(jsxSource, `<Button variant="primary">`, `<Button variant="primary" >`, 1)
		if got := complete(src, `"primary" >`, 10); strings.Join(got, " ") != "disabled=disabled props=props={$1}" {
			t.Fatalf("<Button: %v", got)
		}
		src = strings.Replace(jsxSource, `<Hello name="vuka" />`, `<Hello />`, 1)
		if got := complete(src, `<Hello />`, 7); strings.Join(got, " ") != `name=name="$1"` {
			t.Fatalf("<Hello: %v", got)
		}
	})

	t.Run("completing a closing tag", func(t *testing.T) {
		src := strings.Replace(jsxSource, "since {u.Year}</UserCard>", "since {u.Year}</", 1)
		if got := complete(src, "since {u.Year}</", 16); strings.Join(got, " ") != "UserCard=UserCard>" {
			t.Fatalf("</: %v", got)
		}
	})

	t.Run("completing in braces", func(t *testing.T) {
		src := strings.Replace(jsxSource, "{children}", "{user.}", 1)
		if got := complete(src, "{user.}", 6); !has(got, "Name=Name") || !has(got, "Year=Year") {
			t.Fatalf("{user.}: %v", got)
		}
	})

	t.Run("markup triggers don't complete Go", func(t *testing.T) {
		src := strings.Replace(jsxSource, "func main() {}", "func main() { _ = 1 < }", 1)
		edit(src)
		p := at(src, "1 < }", 3)
		p["context"] = map[string]any{"triggerKind": 2, "triggerCharacter": "<"}
		if got := labels(ask("textDocument/completion", p)); len(got) != 0 {
			t.Fatalf("1 <: %v", got)
		}
	})

	t.Run("an unclosed tag", func(t *testing.T) {
		src := strings.Replace(jsxSource, "<h3>{user.Name}</h3>", "<h3>{user.Name}\n\n", 1)
		edit(src)
		ds := c.waitDiags(uri, func(ds []any) bool {
			b, _ := json.Marshal(ds)
			return strings.Contains(string(b), "at line 18")
		})
		b, _ := json.Marshal(ds)
		if !strings.Contains(string(b), "h3") || !strings.Contains(string(b), "at line 18") || !strings.Contains(string(b), `"start":`+posOf(src, "</div>", 0)) {
			t.Fatalf("diagnostics: %s", b)
		}
		// The rest of the file, lines below the edit included, still works.
		if h := ask("textDocument/hover", at(src, "u.Year", 0)); !strings.Contains(h, "var u User") || !strings.Contains(h, posOf(src, "u.Year", 0)) {
			t.Fatalf("hover below the unclosed tag: %s", h)
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
