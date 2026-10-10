package main

import (
	"encoding/json"
	"path/filepath"
	"regexp"
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
