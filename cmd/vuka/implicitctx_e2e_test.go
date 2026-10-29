package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const implicitCtxPage = `<script lang="go">
package views

import "context"

type Page struct{}

func (p Page) Items() string { return "every item" }

func (p Page) Items(ctx context.Context) string { return "items for the request" }

func (p Page) Title(ctx context.Context) string { return "Shop" }
</script>

<h1>{Title()}</h1>
<p>{Items()}</p>
`

// TestImplicitCtxBuild: vuka build shows a markup call both with and
// without the render's ctx fits (W0108), and vuka explain lists the calls
// given the ctx; with vuka.toml's implicit_ctx = false a call one ctx
// short is E0162 and the build fails.
func TestImplicitCtxBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	files := map[string]string{
		"main.go":              "package main\n\nimport _ \"example.com/app/views\"\n\nfunc main() {}\n",
		"views/page.vuka.html": implicitCtxPage,
	}
	files["go.mod"], files["go.sum"] = testModule(t, "example.com/app")
	root := tempModule(t, files)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)

	var stderr bytes.Buffer
	if err := runGo("build", []string{"-o", filepath.Join(t.TempDir(), "app"), "."}, io.Discard, &stderr); err != nil {
		t.Fatalf("build: %v\n%s", err, stderr.String())
	}
	out := stderr.String()
	if !strings.Contains(out, "warning[W0108]: Items() compiles as written while Items(ctx) also fits") || strings.Contains(out, "Title()") {
		t.Errorf("vuka build says:\n%s", out)
	}

	var explained bytes.Buffer
	if err := explain([]string{filepath.Join(root, "views", "page.vuka.html")}, &explained); err != nil {
		t.Fatalf("explain: %v", err)
	}
	if got := explained.String(); !strings.Contains(got, "page.vuka.html:15:6\tTitle(ctx)") || !strings.Contains(got, "p.Title(ctx)") || !strings.Contains(got, "p.Items0()") {
		t.Errorf("vuka explain says:\n%s", got)
	}

	if err := os.WriteFile(filepath.Join(root, "vuka.toml"), []byte("[compile]\nimplicit_ctx = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if runGo("build", []string{"-o", filepath.Join(t.TempDir(), "app"), "."}, io.Discard, &stderr) == nil {
		t.Fatal("the build passed with implicit ctx off")
	}
	out = stderr.String()
	if !strings.Contains(out, "error[E0162]: Title's first argument (a context.Context) is missing") ||
		!strings.Contains(out, "write ctx explicitly (implicit ctx is off in this module)") || strings.Contains(out, "W0108") {
		t.Errorf("vuka build with implicit ctx off says:\n%s", out)
	}
}
