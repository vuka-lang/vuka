package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

var typistOpenTag = regexp.MustCompile(`(?:^|[\s(={,:>}]|return)<(?:([A-Za-z_][\w.:-]*)(?:\s+[^<>]*[^/<>])?)?>$`)

// typist types target after prefix as VS Code with the extension does: (, {
// and " closed when what follows allows (autoCloseBefore), closers typed
// over, closing tags added on an opening tag's >, Enter indenting and, between
// a pair, putting the closer on its own line. step gets the text typed so far
// with what the editor added after the cursor.
func typist(prefix, target string, step func(buf, typed string)) {
	i, pending := 0, ""
	void := map[string]bool{"br": true, "img": true, "input": true, "hr": true}
	for i < len(target) {
		ch := target[i]
		k := 0
		for k < len(pending) && i+k < len(target) && pending[k] == target[i+k] {
			k++
		}
		for k > 0 && strings.IndexByte(`>)}"`, pending[k-1]) < 0 {
			k--
		}
		switch {
		case k > 0: // typed over, or moved past with the arrow keys
			pending, i = pending[k:], i+k
		case ch == '\n':
			j := i + 1
			for j < len(target) && target[j] == '\t' {
				j++
			}
			if pending != "" && pending[0] != '\n' {
				all := prefix + target[:i]
				ls := strings.LastIndexByte(all, '\n') + 1
				ind := ls
				for ind < len(all) && all[ind] == '\t' {
					ind++
				}
				pending = "\n" + all[ls:ind] + pending
			}
			i = j
		default:
			i++
			next := byte('\n')
			if pending != "" {
				next = pending[0]
			}
			closeOK := strings.IndexByte(";:.,=}])>< \n\t", next) >= 0
			switch {
			case (ch == '{' || ch == '(' || ch == '"') && closeOK:
				pending = string(map[byte]byte{'{': '}', '(': ')', '"': '"'}[ch]) + pending
			case ch == '>':
				all := prefix + target[:i]
				if m := typistOpenTag.FindStringSubmatch(all[strings.LastIndexByte(all, '\n')+1:]); m != nil && !void[m[1]] {
					pending = "</" + m[1] + ">" + pending
				}
			}
		}
		step(target[:i]+pending, target[:i])
	}
}

func TestLSPBenchmark(t *testing.T) {
	start := benchHead + "func PetTable(pets []data.Pet) ui.Node {\n\treturn \n}\n" + benchTail
	c, dir, init := startLSPWith(t, templFiles(t, map[string]string{"main.vuka": start, "data/data.go": benchData}))
	caps, _ := json.Marshal(init.(map[string]any)["capabilities"])
	for _, want := range []string{`"linkedEditingRangeProvider":true`, `"foldingRangeProvider":true`, `"\""`} {
		if !strings.Contains(string(caps), want) {
			t.Fatalf("capabilities lack %s: %.300s", want, caps)
		}
	}
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka", "version": 1, "text": start}})
	version := 1
	edit := func(src string) {
		version++
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": version},
			"contentChanges": []any{map[string]any{"text": src}}})
	}
	// settle is the diagnostics once they stop changing.
	settle := func() []any {
		var last []any
		for {
			select {
			case p := <-c.diags:
				if p["uri"] == uri {
					last, _ = p["diagnostics"].([]any)
				}
			case <-time.After(1200 * time.Millisecond):
				return last
			}
		}
	}
	ask := func(method string, params map[string]any) string {
		b, _ := json.Marshal(c.call(method, params))
		return string(b)
	}
	at := func(src string, off int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(src), off)}
	}
	type item struct {
		Label, Detail, SortText string
		InsertTextFormat        int
		TextEdit                struct{ NewText string }
		Documentation           struct{ Value string }
		Command                 *struct{ Command string }
	}
	complete := func(src string, off int) map[string]item {
		var r struct{ Items []item }
		_ = json.Unmarshal([]byte(ask("textDocument/completion", at(src, off))), &r)
		out := map[string]item{}
		for _, it := range r.Items {
			out[it.Label] = it
		}
		return out
	}
	settle()

	prefix := benchHead + "func PetTable(pets []data.Pet) ui.Node {\n\treturn "
	body := strings.TrimSuffix(strings.TrimPrefix(benchBody, "func PetTable(pets []data.Pet) ui.Node {\n\treturn "), "\n}\n")
	var final string
	checks := map[string]func(src string, off int){
		// 1. completion while typing, the file around mid-edit
		"return <": func(src string, off int) {
			got := complete(src, off)
			if h1 := got["h1"]; !strings.Contains(h1.Documentation.Value, "Heading_Elements") || got["svg"].Detail != "SVG element" || got["search"].Label == "" {
				t.Errorf("after <: h1 %+v, %d items", h1, len(got))
			}
		},
		"\t\t<t": func(src string, off int) {
			got := complete(src, off)
			for _, want := range []string{"table", "td", "tr", "th", "textarea", "template"} {
				if got[want].Label == "" {
					t.Errorf("<t: no %s", want)
				}
			}
		},
		"<th>Age</": func(src string, off int) {
			if got := complete(src, off); got["th"].TextEdit.NewText != "th" && got["th"].TextEdit.NewText != "th>" {
				t.Errorf("</: %v", got)
			}
		},
		"<a ": func(src string, off int) {
			got := complete(src, off)
			href, target := got["href"], got["target"]
			if !strings.HasPrefix(href.SortText, "0") || href.TextEdit.NewText != `href="$1"` || !strings.Contains(href.Documentation.Value, "Element/a#href") {
				t.Errorf("<a : href %+v", href)
			}
			if target.Command == nil || target.Command.Command != "editor.action.triggerSuggest" {
				t.Errorf("<a : target doesn't ask for its values: %+v", target)
			}
			for _, want := range []string{"className", "id", "onClick", "aria-label", "data-", "key"} {
				if got[want].Label == "" {
					t.Errorf("<a : no %s", want)
				}
			}
			if got["colspan"].Label != "" {
				t.Errorf("<a : colspan offered")
			}
			// What other elements take, typed at the same place.
			for tag, want := range map[string]string{"td": "colspan", "input": "placeholder", "label": "htmlFor"} {
				alt := src[:off-2] + tag + " " + src[off:]
				edit(alt)
				if got := complete(alt, off-2+len(tag)+1); got[want].Label == "" {
					t.Errorf("<%s : no %s", tag, want)
				}
				alt = src[:off-2] + tag + ` type="" ` + src[off:]
				if tag == "input" {
					edit(alt)
					if got := complete(alt, off-2+len(tag)+7); got["checkbox"].Label == "" || got["email"].Label == "" {
						t.Errorf(`<input type="|": %v`, got)
					}
				}
				edit(src)
			}
		},
		"<a href={fmt.Spr": func(src string, off int) {
			if got := complete(src, off); got["Sprintf"].Label == "" {
				t.Errorf("fmt.Spr: %v", got)
			}
		},
		`p.ID)}>{p.`: func(src string, off int) {
			if got := complete(src, off); got["Name"].Label == "" || got["Kind"].Label == "" {
				t.Errorf("{p.: %v", got)
			}
		},
		"<td>{p.": func(src string, off int) {
			if got := complete(src, off); got["Kind"].Label == "" || got["Age"].Label == "" {
				t.Errorf("<td>{p.: %v", got)
			}
		},
		"\t\t\t{fo": func(src string, off int) {
			got := complete(src, off)
			if f := got["for"]; f.InsertTextFormat != 2 || !strings.HasPrefix(f.TextEdit.NewText, "for ${1:_}, ${2:it} := range") {
				t.Errorf("{fo: %+v", f)
			}
		},
	}
	typist(prefix, body, func(buf, typed string) {
		src := prefix + buf + "\n}\n" + benchTail
		final = src
		edit(src)
		for k, check := range checks {
			if strings.HasSuffix(typed, k) {
				check(src, len(prefix)+len(typed))
			}
		}
		// 7. diagnostics while typing: never a flood, never stale
		if strings.HasSuffix(typed, "\n") || strings.HasSuffix(typed, "<a hr") || strings.HasSuffix(typed, "<td>{p.") || strings.HasSuffix(typed, "{for _, p") {
			ds := settle()
			b, _ := json.Marshal(ds)
			vuka := strings.Count(string(b), `"source":"vuka"`)
			if vuka > 1 || len(ds) > 2 || strings.Contains(string(b), "undefined") {
				t.Errorf("typed …%q: %s", typed[max(0, len(typed)-30):], b)
			}
		}
	})
	if final != benchSource {
		t.Fatalf("typed:\n%s", final)
	}
	if ds := settle(); len(ds) != 0 {
		t.Fatalf("diagnostics once typed: %v", ds)
	}
	src := benchSource

	// 2. hover
	for needle, want := range map[string]string{"<table": "Tabular data", "</td": "A data cell", "href=": "Element/a#href", "<>": "fragment"} {
		h := ask("textDocument/hover", at(src, strings.Index(src, needle)+len(needle)-1))
		if !strings.Contains(h, want) {
			t.Errorf("hover %s: %s", needle, h)
		}
	}
	if h := ask("textDocument/hover", at(src, nth(t, src, "p.Name", 1, 2))); !strings.Contains(h, "field Name string") {
		t.Errorf("hover p.Name: %s", h)
	}
	// 3. linked editing, and matching tags highlighted
	td := nth(t, src, "<td>", 1, 2)
	if r := ask("textDocument/linkedEditingRange", at(src, td)); !strings.Contains(r, jsonPos(src, nth(t, src, "</td>", 1, 2))) {
		t.Errorf("linkedEditingRange: %s", r)
	}
	if r := ask("textDocument/documentHighlight", at(src, td)); strings.Count(r, `"range"`) != 2 {
		t.Errorf("documentHighlight: %s", r)
	}
	// 5. folding: gopls's function body and imports, markup's elements and blocks
	f := ask("textDocument/foldingRange", map[string]any{"textDocument": map[string]any{"uri": uri}})
	for _, want := range []struct{ from, to string }{
		{"import (", `"lsptest/data"`}, {"func PetTable", "\t</>"}, {"\t\t<table>", "\t\t\t}}"}, {"{for", "\t\t\t\t</tr>"}, {"\t\t\t\t\t<td>\n", "\t\t\t\t\t\t<a"},
	} {
		// gopls ends a fold on its closing line unless the client folds whole lines only.
		end, startLine := lineOf(src, strings.Index(src, want.to)), `,"startLine":`+itoaTest(lineOf(src, strings.Index(src, want.from)))+`}`
		fold := `{"endLine":` + itoaTest(end) + startLine
		if !strings.Contains(f, fold) && !strings.Contains(f, `{"endLine":`+itoaTest(end+1)+startLine) {
			t.Errorf("no fold %s…%s (%s) in %s", want.from, want.to, fold, f)
		}
	}
	// 6. formatting: kept as written, a messy copy fixed, nothing mid-edit
	formatting := func(s string) string {
		edit(s)
		return ask("textDocument/formatting", map[string]any{"textDocument": map[string]any{"uri": uri}, "options": map[string]any{"tabSize": 4, "insertSpaces": false}})
	}
	if r := formatting(src); r != "[]" {
		t.Errorf("formatting the benchmark: %s", r)
	}
	messy := strings.NewReplacer("\t\t\t\t\t", "    ", "{p.Name}", "{ p.Name }", `"/pets/%d", p.ID`, `"/pets/%d",p.ID`, "\t\t<table>", "      <table>").Replace(src)
	var edits []any
	_ = json.Unmarshal([]byte(formatting(messy)), &edits)
	if got := applyEdits(messy, edits); got != src {
		t.Errorf("formatting a messy copy:\n%s", got)
	}
	if r := formatting(strings.Replace(src, "<td>{p.Kind}</td>", "<td>{p.Kind}", 1)); r != "[]" {
		t.Errorf("formatting mid-edit: %s", r)
	}
	edit(src)
	settle()
	// 10. navigation on the Go in the markup
	for _, tc := range []struct {
		needle string
		delta  int
		file   string
		target string
	}{
		{"p.Name", 0, "main.vuka", "p := range"},
		{"pets {", 0, "main.vuka", "pets []"},
		{"data.Pet", 5, "data.go", ""},
		{"fmt.Sprintf", 4, "print.go", ""},
	} {
		d := ask("textDocument/definition", at(src, nth(t, src, tc.needle, 1, tc.delta)))
		if !strings.Contains(d, tc.file) || tc.target != "" && !strings.Contains(d, jsonPos(src, strings.Index(src, tc.target))) {
			t.Errorf("definition of %s: %s", tc.needle, d)
		}
	}
	refs := at(src, nth(t, src, "pets []", 1, 0))
	refs["context"] = map[string]any{"includeDeclaration": true}
	if r := ask("textDocument/references", refs); strings.Count(r, `"uri"`) != 3 {
		t.Errorf("references of pets: %s", r)
	}
	ren := at(src, nth(t, src, "p.Age", 1, 0))
	ren["newName"] = "pet"
	if r := ask("textDocument/rename", ren); strings.Count(r, `"newText":"pet"`) != 5 {
		t.Errorf("rename p: %s", r)
	}
}

// jsonPos is a position as a re-marshalled answer spells it.
func jsonPos(src string, off int) string {
	p := positionOf([]byte(src), off)
	return `{"character":` + itoaTest(p.Character) + `,"line":` + itoaTest(p.Line) + `}`
}

// TestLSPLatency measures how long the language server takes, on a module of
// some size, to report a syntax error and a type error after an edit, and to
// answer a hover and a completion while a rebuild is pending. It reports and
// checks nothing else: run it with -v. VUKA_LSP_BENCH_MODULE and
// VUKA_LSP_BENCH_FILE (relative to the module) measure an existing module
// instead of the generated one.
func TestLSPLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("measures")
	}
	dir, file := os.Getenv("VUKA_LSP_BENCH_MODULE"), os.Getenv("VUKA_LSP_BENCH_FILE")
	opts := lspOptions{renameCommands: true}
	var c *lspClient
	if dir == "" {
		c, dir, _ = startLSPOpts(t, latencyModule(24), opts)
		file = "p00/p00.vuka"
	} else {
		t.Setenv("GOWORK", "off")
		c = startLSPAt(t, dir, opts)
	}
	path := filepath.Join(dir, file)
	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := string(orig)
	uri := pathToURI(path)
	version := 1
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka", "version": version, "text": src}})
	edit := func(text string) time.Time {
		version++
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": version},
			"contentChanges": []any{map[string]any{"text": text}}})
		return time.Now()
	}
	// until waits for diagnostics on the file that satisfy ok.
	until := func(ok func(string) bool) (time.Time, bool) {
		deadline := time.After(30 * time.Second)
		for {
			select {
			case p := <-c.diags:
				if p["uri"] != uri {
					continue
				}
				b, _ := json.Marshal(p["diagnostics"])
				if ok(string(b)) {
					return time.Now(), true
				}
			case <-deadline:
				return time.Now(), false
			}
		}
	}
	// quiet drains diagnostics until none come for a while.
	quiet := func() string {
		last := ""
		for {
			select {
			case p := <-c.diags:
				if p["uri"] == uri {
					b, _ := json.Marshal(p["diagnostics"])
					last = string(b)
				}
			case <-time.After(1500 * time.Millisecond):
				return last
			}
		}
	}
	base := quiet()
	t.Logf("%s: settled; diagnostics %.200s", file, base)
	clean := func(d string) bool { return d == base || d == "null" || d == "[]" }

	const runs = 5
	measure := func(name, add string, ok func(string) bool) {
		var ds []time.Duration
		for i := 0; i < runs; i++ {
			start := edit(src + add + fmt.Sprintf("\n// %d\n", i))
			at, found := until(ok)
			if !found {
				t.Errorf("%s: no diagnostic", name)
				return
			}
			ds = append(ds, at.Sub(start))
			edit(src)
			until(clean)
			quiet()
		}
		t.Logf("%-28s %v", name, median(ds))
	}
	measure("syntax error (Go syntax)", "\nfunc zzSyntax() { ) }\n", func(d string) bool { return strings.Contains(d, `"source":"vuka"`) })
	measure("syntax error (Vuka)", "\n@zz(\nfunc zzAttr() {}\n", func(d string) bool { return strings.Contains(d, `"source":"vuka"`) })
	measure("type error", "\nfunc zzType() int { return \"x\" }\n", func(d string) bool { return strings.Contains(d, "cannot use") })

	// A request while a rebuild is pending: the edit is far from where it asks.
	needle := os.Getenv("VUKA_LSP_BENCH_HOVER")
	if needle == "" {
		needle = "func "
	}
	off := strings.Index(src, needle)
	if off < 0 {
		t.Fatalf("no %q in %s", needle, file)
	}
	off += len(needle) + 1
	at := map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(src), off)}
	for _, method := range []string{"textDocument/hover", "textDocument/completion"} {
		var ds []time.Duration
		answered := 0
		for i := 0; i < runs; i++ {
			edit(src + fmt.Sprintf("\n// pending %d\n", i))
			start := time.Now()
			if v := c.call(method, at); v != nil {
				answered++
			}
			ds = append(ds, time.Since(start))
			quiet()
		}
		t.Logf("%-28s %v (%d/%d answered)", method+" while pending", median(ds), answered, runs)
	}
	edit(src)
	quiet()
	if os.Getenv("VUKA_LSP_BENCH_MODULE") != "" {
		return
	}

	// An exported signature every package uses: a request on one of them is
	// answered while their Go is generated again, and their diagnostics
	// follow the change.
	corePath := filepath.Join(dir, "core", "core.vuka")
	core, _ := os.ReadFile(corePath)
	coreURI := pathToURI(corePath)
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": coreURI, "languageId": "vuka", "version": 1, "text": string(core)}})
	quiet()
	var hovers, follows []time.Duration
	for i := 0; i < runs; i++ {
		text := string(core)
		if i%2 == 0 {
			text = strings.Replace(text, "func Find(id int) Result[User] {", "func Find(id int) User {\n\treturn User{}\n}\n\nfunc find(id int) Result[User] {", 1)
		}
		version++
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": coreURI, "version": version},
			"contentChanges": []any{map[string]any{"text": text}}})
		start := time.Now()
		time.Sleep(buildDelay + 20*time.Millisecond) // the build has started
		h := time.Now()
		c.call("textDocument/hover", at)
		hovers = append(hovers, time.Since(h))
		at, found := until(func(d string) bool { return (i%2 == 0) == strings.Contains(d, `"source":"vuka"`) })
		if !found {
			t.Fatalf("p00's diagnostics don't follow core's change %d", i)
		}
		follows = append(follows, at.Sub(start))
		quiet()
	}
	t.Logf("%-28s %v", "hover while dependents build", median(hovers))
	t.Logf("%-28s %v", "a dependent's diagnostics", median(follows))
}

func median(ds []time.Duration) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	return s[len(s)/2].Round(time.Millisecond)
}

// latencyModule is a module of n packages importing one core package, each
// using what Vuka adds to Go.
func latencyModule(n int) map[string]string {
	files := map[string]string{
		"core/core.vuka": `package core

import "fmt"

type User struct {
	ID   int
	Name string
}

type Shape interface{ Area() float64 }

func Find(id int) Result[User] {
	if id == 0 {
		return Err(fmt.Errorf("no user"))
	}
	return Ok(User{ID: id, Name: fmt.Sprint("user", id)})
}

func Lookup(name string) Option[User] {
	if name == "" {
		return None[User]()
	}
	return Some(User{Name: name})
}
`,
	}
	var imports, calls strings.Builder
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("p%02d", i)
		var b strings.Builder
		fmt.Fprintf(&b, "package %s\n\nimport (\n\t\"fmt\"\n\n\t\"lsptest/core\"\n)\n\n", name)
		for j := 0; j < 12; j++ {
			fmt.Fprintf(&b, `func Name%d(id int) Result[string] {
	u := core.Find(id)?
	return Ok(u.Name)
}

func Describe%d(id int) string {
	match core.Lookup(fmt.Sprint(id)) {
	case Some(u):
		return u.Name
	case None:
		return "none"
	}
}

func area%d(r float64) float64 { return r * r * %d }
func area%d(w, h float64) float64 { return w * h }

`, j, j, j, j+1, j)
		}
		files[name+"/"+name+".vuka"] = b.String()
		fmt.Fprintf(&imports, "\t\"lsptest/%s\"\n", name)
		fmt.Fprintf(&calls, "\tfmt.Println(%s.Describe0(%d))\n", name, i)
	}
	files["main.vuka"] = "package main\n\nimport (\n\t\"fmt\"\n\n" + imports.String() + ")\n\nfunc main() {\n" + calls.String() + "}\n"
	return files
}
