package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// Only the files a web.Frontend call serves from its embedded tree are
// stubbed: not other embeds, not a tree it doesn't embed.
func TestFrontendEmbeds(t *testing.T) {
	root := t.TempDir()
	for p, c := range map[string]string{
		"go.mod": "module example.com/app\n",
		"main.vuka": `package main

import (
	"embed"

	"github.com/vuka-lang/web"
)

//go:embed static
var static embed.FS

// dist is the frontend's build.
//
//go:embed all:web/dist templates/*.html
var dist embed.FS

//go:embed other
var other embed.FS

func main() {
	web.New(web.Frontend(dist, "web/dist", web.FrontendAt("/app")), web.Frontend(other, "nowhere")).Run("")
}
`,
		"web/dist/index.html":          "<html>",
		"web/dist/assets/app.123.js":   "js",
		"web/dist/.vite/manifest.json": "{}",
		"static/logo.svg":              "<svg/>",
		"templates/mail.html":          "<p>",
		"other/x.txt":                  "x",
		"nowhere/y.txt":                "y",
	} {
		writeFile(t, filepath.Join(root, filepath.FromSlash(p)), c)
	}
	got := frontendEmbeds(root)
	var rel []string
	for _, f := range got {
		r, _ := filepath.Rel(root, f)
		rel = append(rel, filepath.ToSlash(r))
	}
	slices.Sort(rel)
	want := []string{"web/dist/.vite/manifest.json", "web/dist/assets/app.123.js", "web/dist/index.html"}
	if !slices.Equal(rel, want) {
		t.Fatalf("stubbed %v, want %v", rel, want)
	}

	tmp := t.TempDir()
	ov, err := stubEmbeds("", tmp, got)
	if err != nil {
		t.Fatal(err)
	}
	var o struct{ Replace map[string]string }
	data, _ := os.ReadFile(ov)
	json.Unmarshal(data, &o)
	if len(o.Replace) != 3 {
		t.Fatalf("overlay: %v", o.Replace)
	}
	for _, to := range o.Replace {
		if fi, err := os.Stat(to); err != nil || fi.Size() != 0 {
			t.Errorf("not an empty stub: %s", to)
		}
	}

	if f, on := stubbing([]string{"-race", "-embed"}); on || len(f) != 1 {
		t.Errorf("-embed: %v %v", f, on)
	}
	t.Setenv("VUKA_EMBED_STUB", "off")
	if _, on := stubbing(nil); on {
		t.Error("VUKA_EMBED_STUB=off")
	}
}
