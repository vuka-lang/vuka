package docs

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
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
		if packagePage.MatchString(string(b)) {
			continue // TestGeneratedPackages
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

// A page marked <!-- vuka:package --> is one package rather than a sample
// per group: its vuka:context blocks (Vuka) and every Vuka sample, each a
// file, go through the repository's own vuka command — vuka eject, which
// also writes what orm generate does for the package's models — and go vet
// checks the Go. A Generated Go tab is then an excerpt of its sample's
// ejected file. The ORM's pages are written this way: their samples share
// the models a page declares once.
const vukaPackageMark = "<!-- vuka:package -->"

// packagePage matches a page with the mark on a line of its own.
var packagePage = regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(vukaPackageMark) + `\s*$`)

var ejectedOut = flag.String("vuka.out", "", "copy each package-mode Vuka sample's ejected Go into this directory")

// packageFile is a file of a package-mode page: a context block or a Vuka
// sample, and the Generated Go tab shown for it.
type packageFile struct {
	name, code string
	line       int
	gen        *sample
}

func TestGeneratedPackages(t *testing.T) {
	type page struct {
		name  string
		files []packageFile
	}
	var pages []page
	for _, p := range docPages(t) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if text := string(b); packagePage.MatchString(text) {
			files, err := packageFiles(p, text)
			if err != nil {
				t.Error(err)
			}
			pages = append(pages, page{p, files})
		}
	}
	if len(pages) == 0 {
		return
	}
	if testing.Short() {
		t.Skip("builds the vuka command")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command")
	}
	root, _ := filepath.Abs("..")
	dir := t.TempDir()
	vukaCmd := buildVuka(t, root, dir)
	mod := filepath.Join(dir, "mod")
	os.MkdirAll(mod, 0o755)
	gomod, err := os.ReadFile("testdata/gomode/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	gomod = bytes.Replace(gomod, []byte("=> ../../.."), []byte("=> "+root), 1)
	os.WriteFile(filepath.Join(mod, "go.mod"), gomod, 0o644)
	sum, _ := os.ReadFile("testdata/gomode/go.sum")
	os.WriteFile(filepath.Join(mod, "go.sum"), sum, 0o644)
	pkgDir := func(i int) string { return fmt.Sprintf("v%02d", i) }
	for i, p := range pages {
		os.MkdirAll(filepath.Join(mod, pkgDir(i)), 0o755)
		for _, f := range p.files {
			os.WriteFile(filepath.Join(mod, pkgDir(i), f.name), []byte(packageSource(f)), 0o644)
		}
	}
	// Errors name the page and the sample's first line rather than the file.
	where := func(out []byte) string {
		s := string(out)
		for i, p := range pages {
			for _, f := range p.files {
				s = strings.ReplaceAll(s, pkgDir(i)+"/"+ejectedName(f.name), fmt.Sprintf("%s (Go of the sample at line %d) ", p.name, f.line))
				s = strings.ReplaceAll(s, pkgDir(i)+"/"+f.name, fmt.Sprintf("%s (the sample at line %d) ", p.name, f.line))
			}
		}
		return s
	}
	env := append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=readonly")
	for _, args := range [][]string{{vukaCmd, "eject", "./..."}, {"go", "vet", "-unreachable=false", "./..."}} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir, cmd.Env = mod, env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s %s: %v\n%s", filepath.Base(args[0]), args[1], err, where(out))
		}
	}
	for i, p := range pages {
		for _, f := range p.files {
			out, err := os.ReadFile(filepath.Join(mod, pkgDir(i), ejectedName(f.name)))
			if err != nil {
				t.Errorf("%s:%d: no Go ejected: %v", p.name, f.line, err)
				continue
			}
			if *ejectedOut != "" {
				os.MkdirAll(*ejectedOut, 0o755)
				name := fmt.Sprintf("%s-%d.go", strings.ReplaceAll(strings.TrimSuffix(p.name, ".md"), "/", "-"), f.line)
				os.WriteFile(filepath.Join(*ejectedOut, name), out, 0o644)
			}
			if f.gen == nil {
				continue
			}
			if missing, ok := excerptOf(f.gen.code, string(out)); !ok {
				t.Errorf("%s:%d: the Generated Go isn't what vuka writes: %q is not in its output, in order. The output:\n%s", p.name, f.gen.line, missing, stripLineDirectives(string(out)))
			}
		}
	}
}

// packageFiles are a package-mode page's context blocks and Vuka samples
// (vuka fences, and html fences opening with <script lang="go">), each
// sample with the Generated Go tab of its code group.
func packageFiles(page, text string) ([]packageFile, error) {
	var files []packageFile
	lines := strings.Split(text, "\n")
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != vukaContextMark {
			continue
		}
		start := i + 2
		var code []string
		for i++; i < len(lines) && strings.TrimSpace(lines[i]) != "-->"; i++ {
			code = append(code, lines[i])
		}
		files = append(files, packageFile{name: fmt.Sprintf("c%04d.vuka", start), code: strings.Join(code, "\n"), line: start})
	}
	pairs, err := generatedPairs(page, text)
	gens := map[int]*sample{}
	for i := range pairs {
		gens[pairs[i].vuka.line] = &pairs[i].gen
	}
	samples, _ := parsePage(page, text)
	for _, s := range samples {
		shape := (s.lang == "html" || s.lang == "vuka-html") && strings.HasPrefix(strings.TrimSpace(s.code), `<script lang="go">`)
		if s.lang != "vuka" && !shape {
			continue
		}
		f := packageFile{name: fmt.Sprintf("s%04d.vuka", s.line), code: s.code, line: s.line, gen: gens[s.line]}
		if shape {
			f.name += ".html"
		}
		files = append(files, f)
	}
	return files, err
}

// packageSource is a file of a package-mode page: package blog, and the
// default imports it names, unless it has a package clause of its own.
func packageSource(f packageFile) string {
	if packageClause.MatchString(f.code) {
		return f.code
	}
	return addDefaultImports("package blog\n\n" + f.code + "\n")
}

// ejectedName is the file vuka eject writes a sample's Go to.
func ejectedName(name string) string {
	if base, ok := strings.CutSuffix(name, ".vuka.html"); ok {
		return base + "_html.go"
	}
	return strings.TrimSuffix(name, ".vuka") + "_vuka.go"
}

// buildVuka builds the repository's vuka command (its own module) against
// this checkout of the libraries it compiles with.
func buildVuka(t *testing.T, root, dir string) string {
	t.Helper()
	work := filepath.Join(dir, "go.work")
	os.WriteFile(work, []byte(fmt.Sprintf("go 1.27.3\n\nuse (\n\t%s\n\t%s\n)\n", root, filepath.Join(root, "cmd", "vuka"))), 0o644)
	bin := filepath.Join(dir, "vuka")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = filepath.Join(root, "cmd", "vuka")
	cmd.Env = append(os.Environ(), "GOWORK="+work)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building vuka: %v\n%s", err, out)
	}
	return bin
}
