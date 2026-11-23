package main

import (
	"path/filepath"
	"strings"
	"testing"
)

const callbackActs = `<script lang="go">
package main

import "context"

// Acts runs a callback.
type Acts struct {
	OnActed func(ctx context.Context) error
	Label   string
}
</script>

<button>{Label}</button>
`

const callbackPage = `<script lang="go">
package main

import (
	"context"
	"strings"

	"github.com/vuka-lang/ui/v2"
)

// Review acts on its orders.
type Review struct {
	ui.Live
	Title string
	count int
}

func load(ctx context.Context) (int, error) { return len(ctx.Value("k").(string)), nil }

// Reset forgets the count.
func (p *Review) Reset() { p.count = 0 }
</script>

<main>
  <Acts label="go" onActed={func(ctx context.Context) error {
    p.count = load(ctx)?
    s := strings.ToUpper(Title)
    _ = s
    return nil
  }}/>
</main>
`

// TestLSPCallbackProp: a Go func literal set on a component's callback
// prop, spanning lines and using ?, is Go to the editor as a method's body
// is: completion of the receiver's fields, a package's and an unimported
// package's members, hover and definition, a type error at its line — and
// a stray ) after the literal reported at the ).
func TestLSPCallbackProp(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n", "review.vuka.html": callbackPage, "acts.vuka.html": callbackActs})
	uri := pathToURI(filepath.Join(dir, "review.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka-html", "version": 1, "text": callbackPage}})
	c.waitDiags(uri, func(ds []any) bool { return !strings.Contains(jsonOf(ds), `"severity":1`) })
	at := func(text, needle string, delta int) map[string]any {
		t.Helper()
		i := strings.Index(text, needle)
		if i < 0 {
			t.Fatalf("no %q", needle)
		}
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": positionOf([]byte(text), i+delta)}
	}
	line := func(text, needle string) string {
		return `"line":` + itoaTest(positionOf([]byte(text), strings.Index(text, needle)).Line)
	}

	if h := jsonOf(c.call("textDocument/hover", at(callbackPage, "Title)", 1))); !strings.Contains(h, "Title string") {
		t.Errorf("hover on a field in the body: %s", h)
	}
	if h := jsonOf(c.call("textDocument/hover", at(callbackPage, "ToUpper", 1))); !strings.Contains(h, "func strings.ToUpper") {
		t.Errorf("hover on a package func in the body: %s", h)
	}
	if d := jsonOf(c.call("textDocument/definition", at(callbackPage, "load(ctx)?", 1))); !strings.Contains(d, line(callbackPage, "func load")) {
		t.Errorf("definition of load from the body, past its ?: %s", d)
	}
	if d := jsonOf(c.call("textDocument/definition", at(callbackPage, "count =", 1))); !strings.Contains(d, line(callbackPage, "count int")) {
		t.Errorf("definition of the receiver's field: %s", d)
	}

	typing := strings.Replace(callbackPage, "    _ = s\n", "    _ = s\n    p.co\n    strings.Has\n    fmt.Spr\n", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2}, "contentChanges": []any{map[string]any{"text": typing}}})
	for needle, want := range map[string]string{"p.co": "count", "strings.Has": "HasPrefix", "fmt.Spr": "Sprintf"} {
		if items := completionLabels(t, c.call("textDocument/completion", at(typing, needle, len(needle)))); items[want] == nil {
			t.Errorf("completion at %s in the body: no %s among %v", needle, want, labelsOfItems(items))
		}
	}

	bad := strings.Replace(callbackPage, "    _ = s\n", "    _ = s\n    var n int = \"x\"\n    _ = n\n", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 3}, "contentChanges": []any{map[string]any{"text": bad}}})
	ds := jsonOf(c.waitDiags(uri, func(ds []any) bool { return strings.Contains(jsonOf(ds), "cannot use") }))
	if !strings.Contains(ds, line(bad, `"x"`)) {
		t.Errorf("type error in the body: %s, want at %s", ds, line(bad, `"x"`))
	}

	typo := strings.Replace(callbackPage, "    return nil\n  }}/>", "    return nil\n  })}/>", 1)
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 4}, "contentChanges": []any{map[string]any{"text": typo}}})
	ds = jsonOf(c.waitDiags(uri, func(ds []any) bool { return strings.Contains(jsonOf(ds), "E0003") }))
	p := positionOf([]byte(typo), strings.Index(typo, ")}/>"))
	if want := `"start":{"character":` + itoaTest(p.Character) + `,"line":` + itoaTest(p.Line) + `}`; !strings.Contains(ds, want) || !strings.Contains(ds, "unexpected ) after the func literal") {
		t.Errorf("stray ): %s, want at %s", ds, want)
	}
}

func labelsOfItems(m map[string]map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
