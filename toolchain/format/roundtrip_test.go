package format

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// roundTripFiles are the repository's .vuka.html files a reader writes —
// examples, conformance apps, scaffolds, golden templates — and the ui
// kit's when VUKA_UI names a checkout.
func roundTripFiles(t *testing.T) map[string][]byte {
	out := map[string][]byte{}
	roots := []string{"../../examples", "../../cmd/vuka/scaffold", "../../transpile/testdata/golden/templates", "../../cmd/vuka/testdata"}
	if ui := os.Getenv("VUKA_UI"); ui != "" {
		roots = append(roots, ui)
	}
	for _, root := range roots {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && (d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".") && p != root) {
				return filepath.SkipDir
			}
			if strings.HasSuffix(p, ".vuka.html") {
				b, _ := os.ReadFile(p)
				out[p] = b
			}
			return nil
		})
	}
	// The docs' complete shape samples.
	filepath.WalkDir("../../docs", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == "node_modules" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if !strings.HasSuffix(p, ".md") {
			return nil
		}
		b, _ := os.ReadFile(p)
		for i, m := range htmlFence.FindAllStringSubmatch(string(b), -1) {
			code := m[1]
			if strings.HasPrefix(strings.TrimSpace(leadingComments.ReplaceAllString(code, "")), `<script lang="go">`) && strings.Contains(code, "\npackage ") &&
				!strings.Contains(code[:strings.Index(code, "</script>")+1], "…") { // an elided sample isn't Go
				out[p+"#"+strconv.Itoa(i)] = []byte(code)
			}
		}
		return nil
	})
	return out
}

var (
	htmlFence       = regexp.MustCompile("(?ms)^```html[^\n]*\n(.*?)^```")
	leadingComments = regexp.MustCompile(`^(\s*<!--[^\n]*-->\s*\n)+`)
)

// TestTemplateRoundTrip: formatting every sample twice changes nothing the
// second time, in its form and rewritten in the script form, and keeps
// what it renders (templateSig, checked by TemplateWith itself).
func TestTemplateRoundTrip(t *testing.T) {
	files := roundTripFiles(t)
	if len(files) < 50 {
		t.Fatalf("only %d files", len(files))
	}
	for p, src := range files {
		for _, form := range []string{"as written", "script"} {
			in := src
			if form == "script" {
				in, _ = transpile.ToScriptForm(src)
			}
			once, err := TemplateWith(in, "", Options{Tailwind: true})
			if err != nil {
				if _, serr := transpile.ParseShape(in); serr != nil {
					continue // a sample of an error
				}
				t.Errorf("%s (%s): %v", p, form, err)
				continue
			}
			twice, err := TemplateWith(once, "", Options{Tailwind: true})
			if err != nil || !bytes.Equal(once, twice) {
				t.Errorf("%s (%s): not idempotent (%v):\n%s\n---\n%s", p, form, err, once, twice)
			}
		}
	}
}

var (
	lineDirectives = regexp.MustCompile(`(?m)^//line .*\n|/\*line [^*]*\*/`)
	spaceRuns      = regexp.MustCompile(`\s+`)
	beforeClose    = regexp.MustCompile(` (/?>)`)
)

// compiled is generated Go as formatting may change it: its line
// directives gone, its runs of whitespace one space, none before a tag's end.
func compiled(b []byte) string {
	s := lineDirectives.ReplaceAllString(string(b), "")
	return beforeClose.ReplaceAllString(spaceRuns.ReplaceAllString(s, " "), "$1")
}

// TestTemplateCompiles: each golden template package compiles to the same
// Go formatted, but for whitespace in its markup's text and its line
// directives.
func TestTemplateCompiles(t *testing.T) {
	dirs, _ := filepath.Glob("../../transpile/testdata/golden/templates/*")
	for _, dir := range dirs {
		entries, _ := os.ReadDir(dir)
		var files, formatted []transpile.File
		for _, e := range entries {
			n := e.Name()
			if !strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, ".vuka") && !strings.HasSuffix(n, ".vuka.html") {
				continue
			}
			src, _ := os.ReadFile(filepath.Join(dir, n))
			files = append(files, transpile.File{Name: n, Src: src})
			if strings.HasSuffix(n, ".vuka.html") {
				if out, err := Template(src, ""); err == nil {
					src = out
				}
			}
			formatted = append(formatted, transpile.File{Name: n, Src: src})
		}
		opts := transpile.Options{Importer: load.NewImporter("../../transpile/testdata/golden", ""), Dir: dir}
		want, werr := transpile.Package(files, opts)
		got, gerr := transpile.Package(formatted, opts)
		if werr != nil || gerr != nil {
			if (werr == nil) != (gerr == nil) {
				t.Errorf("%s: errors %v, formatted %v", dir, werr, gerr)
			}
			continue
		}
		for i, o := range got.Files {
			if compiled(o.Src) != compiled(want.Files[i].Src) {
				t.Errorf("%s: %s differs formatted:\n%s\nwant:\n%s", dir, o.GoName, o.Src, want.Files[i].Src)
			}
		}
	}
}
