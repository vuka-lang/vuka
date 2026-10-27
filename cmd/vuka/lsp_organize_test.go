package main

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestLSPOrganizeImports: source.organizeImports on .vuka and .vuka.html
// files, as an editor asks for it on save, is vuka's: it removes what
// nothing uses, adds what one package can mean, groups the imports, and
// asked again offers nothing. A name with several packages gets none and
// its diagnostic offers each as a quick fix. A request for every action
// offers vuka's organize imports, not gopls's.
func TestLSPOrganizeImports(t *testing.T) {
	files := map[string]string{
		"store/store.vuka": `package store

import (
	"fmt"
	"os"
)

func Name(s string) string {
	return strings.ToUpper(s) + filepath.Ext(s)
}
`,
		"store/sorted.vuka": `package store

import (
	"lsptest/components"
	"github.com/vuka-lang/ui/v2"
	"errors" // the package's own
	"context"
)

var _ = errors.New
var _ context.Context
var _ ui.Node
var _ components.Logo
`,
		"store/broken.vuka": `package store

import "strings"

var n int = "not a number"

func Upper(s string) string { return strings.ToUpper(s) }
`,
		"entities/user.vuka": `package entities

import (
	"fmt"
	"github.com/vuka-lang/orm/v2"
)

type User struct {
	ID int64 @orm.PK
}
`,
		"store/rand.vuka": `package store

func Pick() int { return rand.Intn(3) }
`,
		"components/logo.vuka.html": "<script lang=\"go\">\npackage components\n\ntype Logo struct{}\n</script>\n\n<img src=\"/logo.svg\"/>\n",
		"pages/home.vuka.html": `<script lang="go">
package pages

import "fmt"

type Home struct {
	Name string
}
</script>

<main>
  <components.Logo/>
  <p>{strings.ToUpper(Name)}</p>
</main>
`,
	}
	c, dir, _ := startLSPWith(t, files)
	open := func(name, lang string) string {
		uri := pathToURI(filepath.Join(dir, name))
		c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": lang, "version": 1, "text": files[name]}})
		c.waitDiags(uri, func([]any) bool { return true }) // built: the variables it may read are known
		return uri
	}
	actions := func(uri, text string, only []any, diags []any) []map[string]any {
		ctx := map[string]any{"diagnostics": diags}
		if only != nil {
			ctx["only"] = only
		}
		if diags == nil {
			ctx["diagnostics"] = []any{}
		}
		got := c.call("textDocument/codeAction", map[string]any{"textDocument": map[string]any{"uri": uri},
			"range": lspRange{positionOf([]byte(text), 0), positionOf([]byte(text), len(text))}, "context": ctx})
		var out []map[string]any
		list, _ := got.([]any)
		for _, a := range list {
			out = append(out, a.(map[string]any))
		}
		return out
	}
	onSave := []any{"source.organizeImports"}
	organize := func(name, lang, want string) {
		t.Helper()
		uri := open(name, lang)
		got := actions(uri, files[name], onSave, nil)
		if len(got) != 1 || got[0]["kind"] != "source.organizeImports" {
			t.Fatalf("%s: organize imports: %v", name, got)
		}
		dcs := got[0]["edit"].(map[string]any)["documentChanges"].([]any)
		text := applyEdits(files[name], dcs[0].(map[string]any)["edits"].([]any))
		if text != want {
			t.Fatalf("%s organized:\n%s\nwant:\n%s", name, text, want)
		}
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
			"contentChanges": []any{map[string]any{"text": text}}})
		if again := actions(uri, text, onSave, nil); len(again) != 0 {
			t.Errorf("%s organized twice: %v", name, again)
		}
	}

	organize("store/store.vuka", "vuka", `package store

import (
	"path/filepath"
	"strings"
)

func Name(s string) string {
	return strings.ToUpper(s) + filepath.Ext(s)
}
`)
	organize("pages/home.vuka.html", "vuka-html", `<script lang="go">
package pages

import (
	"strings"

	"lsptest/components"
)

type Home struct {
	Name string
}
</script>

<main>
  <components.Logo/>
  <p>{strings.ToUpper(Name)}</p>
</main>
`)

	organize("store/sorted.vuka", "vuka", `package store

import (
	"context"
	"errors" // the package's own

	"github.com/vuka-lang/ui/v2"

	"lsptest/components"
)

var _ = errors.New
var _ context.Context
var _ ui.Node
var _ components.Logo
`)
	organize("entities/user.vuka", "vuka", `package entities

import "github.com/vuka-lang/orm/v2"

type User struct {
	ID int64 @orm.PK
}
`)
	// A type error elsewhere: the imports stay.
	if got := actions(open("store/broken.vuka", "vuka"), files["store/broken.vuka"], onSave, nil); len(got) != 0 {
		t.Errorf("organized a file with a type error: %v", got)
	}

	// rand: three packages; no import, a quick fix for each.
	uri := open("store/rand.vuka", "vuka")
	if got := actions(uri, files["store/rand.vuka"], onSave, nil); len(got) != 0 {
		t.Errorf("organized an ambiguous name: %v", got)
	}
	diags := c.waitDiags(uri, func(ds []any) bool {
		for _, d := range ds {
			if strings.Contains(d.(map[string]any)["message"].(string), "undefined: rand") {
				return true
			}
		}
		return false
	})
	var titles []string
	for _, a := range actions(uri, files["store/rand.vuka"], []any{"quickfix"}, diags) {
		if strings.HasPrefix(a["title"].(string), "Add import") {
			titles = append(titles, a["title"].(string))
		}
	}
	sort.Strings(titles)
	if want := []string{`Add import "crypto/rand"`, `Add import "math/rand"`, `Add import "math/rand/v2"`}; strings.Join(titles, ",") != strings.Join(want, ",") {
		t.Errorf("quick fixes: %q, want %q", titles, want)
	}

	// Every action: vuka's organize imports, and no gopls's.
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": pathToURI(filepath.Join(dir, "store/store.vuka")), "version": 3},
		"contentChanges": []any{map[string]any{"text": files["store/store.vuka"]}}})
	n := 0
	for _, a := range actions(pathToURI(filepath.Join(dir, "store/store.vuka")), files["store/store.vuka"], nil, nil) {
		if a["kind"] == "source.organizeImports" {
			n++
			if a["title"] != "Organize imports" {
				t.Errorf("gopls's organize imports: %v", a)
			}
		}
	}
	if n != 1 {
		t.Errorf("%d organize imports actions, want vuka's one", n)
	}
}
