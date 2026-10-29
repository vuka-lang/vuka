package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"path/filepath"
	"strings"
	"testing"
)

const staticLSPMain = `package main

import (
	"embed"
	"io/fs"

	"github.com/vuka-lang/web/v2"
)

//go:embed static
var static embed.FS

func main() {
	assets, _ := fs.Sub(static, "static")
	web.New().Static("/static", assets)
}
`

const staticLSPPage = `<script lang="go">
package views

import "github.com/vuka-lang/web/v2"

type Page struct{}
</script>

<img src={web.Asset("/static/images/logo.png")} alt="Logo" />
<img src={web.Asset("/static/im")} />
<img src="/static/images/logo.png" />
`

// TestLSPStaticAssets: inside web.Asset("…") the static files' paths
// complete, a file's hover is its size and dimensions, its definition the
// file; a hard-coded path is W0404 with a quick fix, a missing file E0429.
func TestLSPStaticAssets(t *testing.T) {
	repo, _ := filepath.Abs("../..")
	web := filepath.Join(repo, "transpile", "testdata", "golden", "webretired")
	var logo bytes.Buffer
	png.Encode(&logo, image.NewGray(image.Rect(0, 0, 3, 2)))
	c, dir, _ := startLSPWithMod(t, map[string]string{"main.go": staticLSPMain, "views/page.vuka.html": staticLSPPage,
		"static/images/logo.png": logo.String(), "static/site.css": "body{}"},
		func(mod string) string {
			return strings.Replace(mod, "require (", "require (\n\tgithub.com/vuka-lang/web/v2 v2.0.0", 1) + "\nreplace github.com/vuka-lang/web/v2 => " + web + "\n"
		})
	uri := pathToURI(filepath.Join(dir, "views", "page.vuka.html"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka-html", "version": 1, "text": staticLSPPage}})
	var diags []any
	c.waitDiags(uri, func(ds []any) bool { diags = ds; return len(ds) > 0 })
	got, _ := json.Marshal(diags)
	if !strings.Contains(string(got), `"code":"W0404"`) || !strings.Contains(string(got), `/static/im isn't a static file`) {
		t.Errorf("diagnostics: %s", got)
	}
	at := func(needle string, delta int) map[string]any {
		return map[string]any{"textDocument": map[string]any{"uri": uri},
			"position": positionOf([]byte(staticLSPPage), strings.Index(staticLSPPage, needle)+delta)}
	}

	comp, _ := json.Marshal(c.call("textDocument/completion", at(`/static/im")`, len("/static/im"))))
	if !strings.Contains(string(comp), `"label":"/static/images/logo.png"`) || strings.Contains(string(comp), "site.css") {
		t.Errorf("completion: %s", comp)
	}
	hover, _ := json.Marshal(c.call("textDocument/hover", at(`"/static/images/logo.png"`, 3)))
	if !strings.Contains(string(hover), "static/images/logo.png") || !strings.Contains(string(hover), "3×2") {
		t.Errorf("hover: %s", hover)
	}
	def, _ := json.Marshal(c.call("textDocument/definition", at(`"/static/images/logo.png"`, 3)))
	if !strings.Contains(string(def), "static/images/logo.png") {
		t.Errorf("definition: %s", def)
	}

	off := strings.Index(staticLSPPage, `src="/static`)
	r := lspRange{positionOf([]byte(staticLSPPage), off), positionOf([]byte(staticLSPPage), off+5)}
	acts, _ := json.Marshal(c.call("textDocument/codeAction", map[string]any{"textDocument": map[string]any{"uri": uri},
		"range": r, "context": map[string]any{"diagnostics": []any{}, "only": []string{"quickfix"}}}))
	if !strings.Contains(string(acts), `Link /static/images/logo.png with web.Asset`) || !strings.Contains(string(acts), `{web.Asset(\"/static/images/logo.png\")}`) {
		t.Errorf("code actions: %s", acts)
	}
}
