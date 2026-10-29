package docs

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// A page shows what Vuka becomes with a code group of two tabs: the Vuka
// source, and the Go the transpiler makes of it.
//
//	<!-- vuka:context
//	Go declarations the source uses without showing them; the last one
//	before a group is its context
//	-->
//	::: code-group
//	```vuka [Vuka]
//	…
//	```
//	```go [Generated Go]
//	…
//	```
//	:::
//
// The Vuka is transpiled as a file of a package (package p when it has no
// package clause; sample.vuka, or sample.vuka.html for a vuka-html or html
// fence) beside the context, against the libraries the Go samples are
// checked with; a file without imports imports the packages it names
// among the Go samples' default imports (fmt., errors., web.). The Go tab
// is that output trimmed: its lines, spaces
// aside, appear in the output in the same order, and a line that is only
// "…" or "// …" stands for lines left out.

const vukaContextMark = "<!-- vuka:context"

var fenceLabel = regexp.MustCompile("^\\s*(?:`{3,}|~{3,})\\s*[\\w-]*\\s*\\[([^\\]]+)\\]")

// generatedPair is one code group: its Vuka sample, the Go shown for it and
// the hidden context before it.
type generatedPair struct {
	vuka, gen sample
	context   string
}

// generatedPairs are a page's Vuka/Generated Go code groups.
func generatedPairs(page, text string) ([]generatedPair, error) {
	samples, containers := parsePage(page, text)
	lines := strings.Split(text, "\n")
	label := func(s sample) string {
		if m := fenceLabel.FindStringSubmatch(lines[s.line-2]); m != nil {
			return strings.TrimSpace(m[1])
		}
		return ""
	}
	var out []generatedPair
	for _, c := range containers {
		if c.name != "code-group" {
			continue
		}
		var in []sample
		for _, s := range samples {
			if s.line > c.line && s.line < c.end {
				in = append(in, s)
			}
		}
		var p generatedPair
		found := false
		for _, s := range in {
			switch {
			case label(s) == "Generated Go":
				p.gen, found = s, true
			case s.lang == "vuka" || s.lang == "vuka-html" || s.lang == "html":
				p.vuka = s
			}
		}
		if !found {
			continue
		}
		if p.vuka.code == "" {
			return nil, fmt.Errorf("%s:%d: a Generated Go tab without a Vuka tab before it", page, p.gen.line)
		}
		// The context is the last vuka:context comment before the group.
		for i := c.line - 2; i >= 0; i-- {
			if strings.TrimSpace(lines[i]) != vukaContextMark {
				continue
			}
			end := i + 1
			for end < len(lines) && strings.TrimSpace(lines[end]) != "-->" {
				end++
			}
			p.context = strings.Join(lines[i+1:end], "\n")
			break
		}
		out = append(out, p)
	}
	return out, nil
}

// generatedGoLines are the lines of the pages' Generated Go tabs, which the
// Go sample check leaves to TestGeneratedGo: they are excerpts.
func generatedGoLines(page, text string) map[int]bool {
	pairs, _ := generatedPairs(page, text)
	out := map[int]bool{}
	for _, p := range pairs {
		out[p.gen.line] = true
	}
	return out
}

var (
	packageClause = regexp.MustCompile(`(?m)^package \w+`)
	lineDirective = regexp.MustCompile(`^//line `)
	spaces        = regexp.MustCompile(`\s+`)
)

func TestGeneratedGo(t *testing.T) {
	var pairs []generatedPair
	for _, page := range docPages(t) {
		b, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		ps, err := generatedPairs(page, string(b))
		if err != nil {
			t.Error(err)
		}
		pairs = append(pairs, ps...)
	}
	if len(pairs) == 0 {
		return
	}
	dir := t.TempDir()
	mod, err := os.ReadFile("testdata/gomode/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.Abs("..")
	mod = []byte(strings.Replace(string(mod), "=> ../../..", "=> "+root, 1))
	os.WriteFile(filepath.Join(dir, "go.mod"), mod, 0o644)
	sum, _ := os.ReadFile("testdata/gomode/go.sum")
	os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644)
	os.WriteFile(filepath.Join(dir, "doc.go"), []byte("package docsamples\n"), 0o644)

	for _, p := range pairs {
		src := p.vuka.code
		pkg := "p"
		if m := packageClause.FindString(src); m != "" {
			pkg = strings.TrimPrefix(m, "package ")
		} else if p.vuka.lang == "vuka" {
			src = "package p\n\n" + src
		}
		if p.vuka.lang == "vuka" {
			src = addDefaultImports(src)
		}
		name := "sample.vuka"
		if p.vuka.lang != "vuka" {
			name = "sample.vuka.html"
		}
		files := []transpile.File{{Name: name, Src: []byte(src)}}
		if p.context != "" {
			ctx := p.context
			if !packageClause.MatchString(ctx) {
				ctx = "package " + pkg + "\n\n" + ctx
			}
			ctx = addDefaultImports(ctx)
			files = append(files, transpile.File{Name: "context.go", Src: []byte(ctx)})
		}
		res, err := transpilePackage(dir, files)
		if err != nil {
			t.Errorf("%s:%d: the Vuka doesn't transpile: %v", p.vuka.page, p.vuka.line, err)
			continue
		}
		var got string
		for _, f := range res.Files {
			if f.Name == name {
				got = string(f.Src)
			}
		}
		if missing, ok := excerptOf(p.gen.code, got); !ok {
			t.Errorf("%s:%d: the Generated Go isn't the transpiler's: %q is not in its output, in order. The output:\n%s", p.gen.page, p.gen.line, missing, stripLineDirectives(got))
		}
	}
}

// excerptOf reports whether shown's lines appear in out's, in order,
// spaces aside; missing is the first that doesn't.
func excerptOf(shown, out string) (missing string, ok bool) {
	var have []string
	for _, l := range strings.Split(out, "\n") {
		if !lineDirective.MatchString(l) {
			have = append(have, norm(l))
		}
	}
	i := 0
	for _, l := range strings.Split(shown, "\n") {
		n := norm(l)
		if n == "" || n == "…" || n == "// …" {
			continue
		}
		for i < len(have) && have[i] != n {
			i++
		}
		if i == len(have) {
			return strings.TrimSpace(l), false
		}
		i++
	}
	return "", true
}

func norm(l string) string { return strings.TrimSpace(spaces.ReplaceAllString(l, " ")) }

func stripLineDirectives(s string) string {
	var keep []string
	for _, l := range strings.Split(s, "\n") {
		if !lineDirective.MatchString(l) {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

// transpilePackage transpiles files as one package, resolving imports
// against the module at dir.
func transpilePackage(dir string, files []transpile.File) (*transpile.Result, error) {
	return transpile.Package(files, transpile.Options{Importer: exportImporter(dir)})
}

var (
	importDecl = regexp.MustCompile(`(?m)^import\b`)
	qualified  = regexp.MustCompile(`\b([a-z]\w*)\.[A-Za-z_]`)
	strOrNote  = regexp.MustCompile("\"(\\\\.|[^\"\\\\\n])*\"|`[^`]*`|//[^\n]*")
)

// addDefaultImports is src, which has a package clause, importing the default
// imports it names, when it imports nothing itself.
func addDefaultImports(src string) string {
	if importDecl.MatchString(src) {
		return src
	}
	code := strOrNote.ReplaceAllString(src, "")
	seen := map[string]bool{}
	var imports []string
	for _, m := range qualified.FindAllStringSubmatch(code, -1) {
		if path, ok := defaultImports[m[1]]; ok && !seen[m[1]] {
			seen[m[1]] = true
			if path[strings.LastIndexByte(path, '/')+1:] == m[1] {
				imports = append(imports, fmt.Sprintf("import %q", path))
			} else {
				imports = append(imports, fmt.Sprintf("import %s %q", m[1], path))
			}
		}
	}
	if len(imports) == 0 {
		return src
	}
	loc := packageClause.FindStringIndex(src)
	return src[:loc[1]] + "\n\n" + strings.Join(imports, "\n") + "\n" + src[loc[1]:]
}
