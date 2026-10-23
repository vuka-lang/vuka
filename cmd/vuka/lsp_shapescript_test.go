package main

import (
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
