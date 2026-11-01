package main

import (
	"os"
	"strings"
	"testing"
)

const headerShape = "package pages\n\n// Index is the home page.\ntype Index struct {\n\tTitle string\n}\n<main>\n  <h1>{Title}</h1>\n</main>\n"

const scriptShape = "<!-- the home page -->\n<script lang=\"go\">\npackage pages\n\n// Index is the home page; \"</script>\" doesn't end it.\ntype Index struct {\n\tTitle string\n}\n</script>\n\n<main>\n  <h1>{Title}</h1>\n</main>\n"

// TestShapeBoundsForms: the editor reads both forms' boundary as the
// compiler does.
func TestShapeBoundsForms(t *testing.T) {
	for _, src := range []string{headerShape, scriptShape} {
		sh, _, markup := shapeBounds([]byte(src))
		if sh == nil || sh.Type != "Index" || !strings.HasPrefix(src[markup:], "<main>") {
			t.Errorf("shapeBounds: %+v %d\n%s", sh, markup, src)
		}
		if shapeMarkup([]byte(src)) != markup {
			t.Errorf("shapeMarkup %d, want %d", shapeMarkup([]byte(src)), markup)
		}
		in := markupText([]byte(src))
		if at := strings.Index(src, "home page"); in(at) {
			t.Errorf("markupText: the Go's comment is markup")
		}
	}
	sp, ok := splitShape([]byte(scriptShape))
	if !ok || !strings.HasPrefix(scriptShape[sp.goStart:], "<script") || !strings.HasSuffix(scriptShape[:sp.goEnd], "</script>") {
		t.Errorf("splitShape: %+v", sp)
	}
	if start, end, ok := headerFold([]byte(scriptShape)); !ok || start != 1 || end != 8 {
		t.Errorf("fold %d-%d", start, end)
	}
}

// TestShapeHelperStructs: the editor reads a file of several structs as the
// compiler does: the struct named after the file is the one its markup's
// names belong to; the others are no components.
func TestShapeHelperStructs(t *testing.T) {
	dir := t.TempDir()
	const src = "<script lang=\"go\">\npackage pages\n\ntype Row struct {\n\tName string\n}\n\nfunc (r Row) Label() string { return r.Name }\n\ntype Orders struct {\n\tTitle string\n}\n\nfunc (o Orders) Count() int { return 1 }\n</script>\n\n<h1>{Title} {Count()}</h1>\n"
	file := dir + "/orders.vuka.html"
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	s := scanShape(file, []byte(src))
	if s.shape == nil || s.shape.Type != "Orders" || s.fields["Title"] == nil || s.fields["Name"] != nil || s.methods["Count"] == nil || s.methods["Label"] != nil {
		t.Errorf("scan: %+v", s)
	}
	if r, ok := s.refAt(strings.Index(src, "Title}")); !ok || r.kind != refProp {
		t.Errorf("{Title}: %+v %v", r, ok)
	}
	if shapeMarkup([]byte(src)) != strings.Index(src, "<h1>") {
		t.Errorf("shapeMarkup: %d", shapeMarkup([]byte(src)))
	}
	if info := tagComponentIn(dir, "Orders"); info == nil {
		t.Error("Orders is no component")
	}
	if info := tagComponentIn(dir, "Row"); info != nil {
		t.Errorf("the helper is a component: %+v", info)
	}
	comps := dirComponents(dir)
	if len(comps) != 1 || comps[0].name != "Orders" {
		t.Errorf("components: %+v", comps)
	}
}

// TestShapeScriptAction: a header-form shape offers the rewrite; a
// script-form one doesn't.
func TestShapeScriptAction(t *testing.T) {
	p := &proxy{shown: map[string]string{}}
	vf := &vfile{source: t.TempDir() + "/index.vuka.html", from: []byte(headerShape)}
	got, _ := p.withShapeScriptAction(vf, nil).([]any)
	if len(got) != 1 {
		t.Fatalf("actions: %v", got)
	}
	a := got[0].(map[string]any)
	edit := a["edit"].(map[string]any)["changes"].(map[string]any)
	for _, es := range edit {
		text := es.([]any)[0].(map[string]any)["newText"].(string)
		if !strings.HasPrefix(text, "<script lang=\"go\">\npackage pages\n") || !strings.Contains(text, "</script>\n\n<main>") {
			t.Errorf("rewrite:\n%s", text)
		}
	}
	vf.from = []byte(scriptShape)
	if got := p.withShapeScriptAction(vf, nil); got != nil {
		t.Errorf("a script-form file: %v", got)
	}
}
