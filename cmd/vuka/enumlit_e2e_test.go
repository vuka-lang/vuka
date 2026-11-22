package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var enumLitModule = map[string]string{
	"main.go": `package main

import (
	"context"
	"fmt"

	"example.com/app/views"
	"github.com/vuka-lang/ui/v2"
)

func main() {
	s, err := ui.String(context.Background(), &views.Index{})
	if err != nil {
		panic(err)
	}
	fmt.Print(s)
}
`,
	"design/button.vuka.html": `<script lang="go">
package design

import "github.com/vuka-lang/ui/v2"

// Variant is a Button's look.
type Variant string

const (
	VariantDefault Variant = ""
	VariantOutline Variant = "outline"
)

// Accent is a colour: a preset or any CSS colour.
//
//vuka:open
type Accent string

const AccentBrand Accent = "brand"

type Button struct {
	Variant  Variant
	Accent   Accent
	Children ui.Node
}
</script>

<button class={string(Variant)} data-accent={string(Accent)}>{Children}</button>
`,
	"views/index.vuka.html": `<script lang="go">
package views

import "example.com/app/design"

type Index struct{}
</script>

<main>
  <design.Button variant="outlien" accent="#ff0000">Cancel</design.Button>
</main>
`,
}

// TestEnumLiteralsBuild: a quoted value a prop's type of another Vuka
// package doesn't declare fails the build at the attribute, and the type's
// //vuka:open, read from that package's template file, lets any value by.
func TestEnumLiteralsBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	files := map[string]string{}
	for k, v := range enumLitModule {
		files[k] = v
	}
	files["go.mod"], files["go.sum"] = testModule(t, "example.com/app")
	root := tempModule(t, files)
	t.Setenv("GOFLAGS", "")
	t.Chdir(root)
	var stderr bytes.Buffer
	if runGo("build", []string{"-o", filepath.Join(t.TempDir(), "app"), "."}, io.Discard, &stderr) == nil {
		t.Fatal("the build passed")
	}
	out := stderr.String()
	if !strings.Contains(out, "error[E0196]") || !strings.Contains(out, "views/index.vuka.html:10:26") || !strings.Contains(out, `did you mean "outline"?`) || strings.Count(out, "E0196]") != 1 {
		t.Fatalf("build:\n%s", out)
	}
	index := filepath.Join(root, "views", "index.vuka.html")
	src, _ := os.ReadFile(index)
	os.WriteFile(index, bytes.Replace(src, []byte(`"outlien"`), []byte(`"outline"`), 1), 0o644)
	stderr.Reset()
	var stdout bytes.Buffer
	if err := runGo("run", []string{"."}, &stdout, &stderr); err != nil {
		t.Fatalf("vuka run: %v\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `class="outline" data-accent="#ff0000"`) {
		t.Errorf("rendered:\n%s", stdout.String())
	}
}
