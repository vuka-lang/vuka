package docs

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// The pages' Go samples are code a reader copies: every Go fence is
// type-checked and vetted against the
// libraries at main (testdata/gomode's module), and none of them may use
// what only a .vuka file has. A sample is a file; one marked
// <!-- go:fragment --> is statements, checked as a function's body with ctx
// in scope. A sample sees the declarations of the page's other samples, so a
// page declares Post once and uses it after.

var pages = flag.String("pages", "", "check only these pages (globs, comma-separated: web/*.md)")

// sample is a fenced block of a page.
type sample struct {
	page     string // web/routes.md
	line     int    // of the first line of code
	lang     string
	code     string
	inGo     bool // inside ::: go
	inVuka   bool // inside ::: vuka
	fragment bool
}

// The two marks a page's Markdown may hold for its Go samples: the next
// sample is a fragment, and declarations the page's samples use without
// showing them (<!-- go:context, Go, then --> on a line of its own).
const (
	fragmentMark = "<!-- go:fragment -->"
	contextMark  = "<!-- go:context"
)

// docPages are the published pages: every .md but the design notes and RFCs.
func docPages(t *testing.T) []string {
	var out []string
	filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (p == "node_modules" || p == "design" || p == "rfc" || p == "testdata" || strings.HasPrefix(d.Name(), ".") && p != ".") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(p, ".md") {
			out = append(out, filepath.ToSlash(p))
		}
		return nil
	})
	if len(out) == 0 {
		t.Fatal("no pages")
	}
	if *pages != "" {
		var keep []string
		for _, p := range out {
			for _, g := range strings.Split(*pages, ",") {
				if ok, _ := filepath.Match(g, p); ok {
					keep = append(keep, p)
					break
				}
			}
		}
		out = keep
	}
	return out
}

var (
	containerOpen = regexp.MustCompile(`^(:{3,})\s*(\S+)`)
	containerEnd  = regexp.MustCompile(`^:{3,}\s*$`)
	fenceOpen     = regexp.MustCompile("^\\s*(`{3,}|~{3,})\\s*([\\w-]*)")
)

// container is a ::: block: its name and first and last lines.
type container struct {
	name      string
	line, end int
}

// parsePage is a page's fenced samples and its containers.
func parsePage(page, text string) ([]sample, []container) {
	var (
		out      []sample
		stack    []container
		done     []container
		lines    = strings.Split(text, "\n")
		lastText string
	)
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == contextMark {
			s := sample{page: page, line: i + 2, lang: "go", inGo: true}
			var code []string
			for i++; i < len(lines) && strings.TrimSpace(lines[i]) != "-->"; i++ {
				code = append(code, lines[i])
			}
			s.code = strings.Join(code, "\n")
			out = append(out, s)
			continue
		}
		if m := fenceOpen.FindStringSubmatch(l); m != nil {
			fence := strings.TrimLeft(m[1], " \t")
			s := sample{page: page, line: i + 2, lang: m[2], fragment: strings.TrimSpace(lastText) == fragmentMark}
			for _, c := range stack {
				s.inGo = s.inGo || c.name == "go"
				s.inVuka = s.inVuka || c.name == "vuka"
			}
			var code []string
			for i++; i < len(lines); i++ {
				t := strings.TrimSpace(lines[i])
				if strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
					break
				}
				code = append(code, lines[i])
			}
			s.code = strings.Join(code, "\n")
			out = append(out, s)
			lastText = "```"
			continue
		}
		switch t := strings.TrimSpace(l); {
		case containerEnd.MatchString(t) && len(stack) > 0:
			c := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			c.end = i + 1
			done = append(done, c)
		case containerOpen.MatchString(t):
			stack = append(stack, container{name: containerOpen.FindStringSubmatch(t)[2], line: i + 1})
		}
		if strings.TrimSpace(l) != "" {
			lastText = l
		}
	}
	sort.Slice(done, func(i, j int) bool { return done[i].line < done[j].line })
	return out, done
}

// vukaOnly are what a .vuka file has and Go doesn't, or older API: none of
// them may appear in an ordinary Go sample.
var vukaOnly = []struct {
	re    *regexp.Regexp
	help  string
	names bool // matched in code only: orm.Q{"age__lt": 30} is Go
}{
	{regexp.MustCompile(`\b[A-Z]\w*\.Objects\b`), "a model's manager is orm.Objects[T]()", false},
	{regexp.MustCompile(`\b\w*[A-Za-z0-9]__\w|\b__\w`), "__ names are the transpiler's; Go names them (UpdateNewMessage, areaCircle)", true},
	{regexp.MustCompile(`\bSelf\b`), "Self is .vuka's; name the type", false},
	{regexp.MustCompile("`[^`\n]*\\b(bind|defer|url):\""), "declare live field attributes in the shape header", false},
	{regexp.MustCompile(`\bui\.Template\b`), "markup belongs in a .vuka.html shape", false},
	{regexp.MustCompile(`\{\{\d+\}\}`), "shape holes use expressions: {Title}", false},
	{regexp.MustCompile(`\bui\.(Alert|Badge|Button|Card|DataTable|Form|Field|Image|Modal|Tabs|Table|ToastContainer|Toast|Tile)Props\b`), "kit components are structs: ui.Card{…}", false},
	{regexp.MustCompile(`\}\.Render\(\)`), "a struct component is itself a ui.Node: no .Render()", false},
	{regexp.MustCompile(`(?m)^\s*@(\w+\.)?[A-Za-z]\w*[({]|^\s*@\w+\.[A-Z]\w*\s*$`), "decorators belong in .vuka logic or .vuka.html headers; use vuka.Wrap for Go call decorators", false},
	{regexp.MustCompile(`\bmatch\s+\w[^{\n]*\{`), "match is a switch in Go", false},
}

var stringLit = regexp.MustCompile("\"(\\\\.|[^\"\\\\\n])*\"|`[^`]*`")

func TestGoSamples(t *testing.T) {
	var all [][]sample
	for _, page := range docPages(t) {
		b, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		samples, containers := parsePage(page, string(b))
		checkContainers(t, page, string(b), samples, containers)
		var vet []sample
		for _, s := range samples {
			shown := !s.inVuka
			if !shown || strings.HasPrefix(page, "errors/") {
				continue
			}
			if s.lang == "go" || s.lang == "html" && s.inGo {
				for _, v := range vukaOnly {
					code := s.code
					if v.names {
						code = stringLit.ReplaceAllStringFunc(code, func(q string) string { return strings.Repeat(" ", len(q)) })
					}
					if loc := v.re.FindStringIndex(code); loc != nil {
						t.Errorf("%s:%d: %q in a Go sample: %s", page, s.line+strings.Count(s.code[:loc[0]], "\n"), s.code[loc[0]:loc[1]], v.help)
					}
				}
			}
			if s.inGo && (s.lang == "vuka" || s.lang == "jsx") {
				t.Errorf("%s:%d: a %s sample under ::: go", page, s.line, s.lang)
			}
			if s.lang == "go" {
				vet = append(vet, s)
			}
		}
		if page == "ui/testing.md" {
			for _, shape := range samples {
				if shape.lang == "html" && strings.Contains(shape.code, "package components") {
					shape.lang = "shape"
					vet = append(vet, shape)
				}
			}
		}
		if len(vet) > 0 {
			all = append(all, vet)
		}
	}
	if len(all) == 0 {
		return
	}
	vetSamples(t, all)
}

// checkContainers rejects the retired preference containers in published guides.
func checkContainers(t *testing.T, page, text string, samples []sample, cs []container) {
	if strings.HasPrefix(page, "errors/") {
		return
	}
	for _, c := range cs {
		if c.name == "vuka" || c.name == "go" {
			t.Errorf("%s:%d: retired sample preference container %s", page, c.line, c.name)
		}
	}
}

// vetSamples writes each page's Go samples as packages of a module and vets
// them.
func vetSamples(t *testing.T, all [][]sample) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go command")
	}
	root, _ := filepath.Abs("..")
	dir := t.TempDir()
	mod, err := os.ReadFile("testdata/gomode/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	mod = bytes.Replace(mod, []byte("=> ../../.."), []byte("=> "+root), 1)
	// A generated page package backs the copyable main.go samples. Its shape
	// is read from the actual guide, so startup is checked against a real node.
	mod = append(mod, []byte("\nrequire example.com/app v0.0.0\nreplace example.com/app => ./exampleapp\n")...)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), mod, 0o644); err != nil {
		t.Fatal(err)
	}
	sums, _ := os.ReadFile("testdata/gomode/go.sum")
	os.WriteFile(filepath.Join(dir, "go.sum"), sums, 0o644)
	guide, err := os.ReadFile("web/index.md")
	if err != nil {
		t.Fatal(err)
	}
	guideSamples, _ := parsePage("web/index.md", string(guide))
	fixture := filepath.Join(dir, "exampleapp", "pages")
	if err := os.MkdirAll(fixture, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "exampleapp", "go.mod"), []byte("module example.com/app\ngo 1.27\n"), 0o644)
	found := false
	for _, shape := range guideSamples {
		if shape.lang != "html" || !strings.Contains(shape.code, "type Index struct") {
			continue
		}
		result, err := transpileFixture(t, dir, "index.vuka.html", shape.code)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range result.Files {
			if err := os.WriteFile(filepath.Join(fixture, f.GoName), f.Src, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		found = true
	}
	if !found {
		t.Fatal("web/index.md must provide the startup fixture's Index shape")
	}
	// The folder generator's exported contract is Routes() web.Option.
	// Register the fixture page using the same page-route option it emits.
	route := `package pages
import "github.com/vuka-lang/web/v2"
func Routes() web.Option {
 return web.OptionFunc(func(app *web.App) {
  app.Get("/", func() *Index { return &Index{} }, web.PageRoute())
 })
}
`
	if err := os.WriteFile(filepath.Join(fixture, "route.go"), []byte(route), 0o644); err != nil {
		t.Fatal(err)
	}
	// The app's users package, whose context processor the guide to
	// context processors lists.
	users := `package users

import "context"

type User struct {
	ID   string
	Name string
}

type Service struct{}

func (s *Service) ByID(ctx context.Context, id string) (*User, error) { return &User{ID: id}, nil }

type Vars struct{ Me *User }

func Context(ctx context.Context, s *Service) (Vars, error) { return Vars{}, nil }
`
	os.MkdirAll(filepath.Join(dir, "exampleapp", "users"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "exampleapp", "users", "users.go"), []byte(users), 0o644); err != nil {
		t.Fatal(err)
	}

	sum, _ := os.ReadFile("testdata/gomode/go.sum")
	os.WriteFile(filepath.Join(dir, "go.mod"), mod, 0o644)
	os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644)
	n := 0
	for _, page := range all {
		for i := range page {
			if page[i].lang != "shape" {
				continue
			}
			result, err := transpileFixture(t, dir, "greeting.vuka.html", page[i].code)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Files) != 1 {
				t.Fatal("expected one generated shape")
			}
			page[i].code = string(result.Files[0].Src)
			page[i].lang = "go"
		}
		for i := range page {
			files, err := samplePackage(page, i)
			if err != nil {
				t.Error(err)
				continue
			}
			n++
			pkg := filepath.Join(dir, fmt.Sprintf("p%03d", n))
			os.MkdirAll(pkg, 0o755)
			for name, src := range files {
				os.WriteFile(filepath.Join(pkg, name), src, 0o644)
				embedFiles(pkg, src)
			}
		}
	}
	env := append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=readonly")
	// What //go:generate writes for the models: their typed fields
	// (PostFields) and scanners.
	gen := exec.Command("go", "tool", "orm", "generate", ".")
	gen.Dir, gen.Env = dir, env
	if out, err := gen.CombinedOutput(); err != nil {
		t.Fatalf("orm generate: %v\n%s", err, out)
	}
	cmd := exec.Command("go", "vet", "-unreachable=false", "./...")
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.CombinedOutput()
	if err != nil {
		var keep []string
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if !strings.HasPrefix(l, "# ") {
				l = strings.TrimPrefix(l, "vet: ")
				if i := strings.IndexByte(l, '/'); i > 0 && strings.HasPrefix(l, "p") {
					l = l[i+1:]
				}
				keep = append(keep, l)
			}
		}
		t.Errorf("Go samples don't vet:\n%s", strings.Join(keep, "\n"))
	}
}

var embedLine = regexp.MustCompile(`(?m)^//go:embed (.+)$`)

// embedFiles makes what a sample's //go:embed lines name: a file for a
// name with an extension, else a directory holding one.
func embedFiles(pkg string, src []byte) {
	for _, m := range embedLine.FindAllSubmatch(src, -1) {
		for _, p := range strings.Fields(string(m[1])) {
			p = strings.TrimPrefix(p, "all:")
			if strings.ContainsAny(p, "*?[") {
				continue
			}
			if filepath.Ext(p) == "" {
				p = filepath.Join(p, "index.html")
			}
			os.MkdirAll(filepath.Join(pkg, filepath.Dir(p)), 0o755)
			os.WriteFile(filepath.Join(pkg, p), nil, 0o644)
		}
	}
}

// decl is a top-level declaration of a sample.
type decl struct {
	key  string // Name, or Recv.Name for a method
	src  string
	line int
}

var defaultImports = map[string]string{
	"context": "context", "errors": "errors", "fmt": "fmt", "strings": "strings", "time": "time",
	"http": "net/http", "httptest": "net/http/httptest", "log": "log", "os": "os", "strconv": "strconv",
	"slices": "slices", "sort": "sort", "embed": "embed", "io": "io", "sync": "sync", "testing": "testing",
	"json": "encoding/json", "math": "math", "slog": "log/slog", "fs": "io/fs", "url": "net/url",
	"utf8": "unicode/utf8", "sql": "database/sql", "sha1": "crypto/sha1", "sha256": "crypto/sha256", "subtle": "crypto/subtle", "hex": "encoding/hex", "base64": "encoding/base64", "driver": "database/sql/driver", "regexp": "regexp", "filepath": "path/filepath", "iter": "iter", "maps": "maps", "rand": "math/rand/v2", "atomic": "sync/atomic", "bytes": "bytes",
	"templ": "github.com/a-h/templ",
	"vuka":  "github.com/vuka-lang/vuka/v2",
	"ui":    "github.com/vuka-lang/ui/v2", "live": "github.com/vuka-lang/ui/v2/live", "livetest": "github.com/vuka-lang/ui/v2/live/livetest",
	"validate": "github.com/vuka-lang/ui/v2/validate", "term": "github.com/vuka-lang/ui/v2/term", "templx": "github.com/vuka-lang/ui/v2/templx",
	"web": "github.com/vuka-lang/web/v2", "auth": "github.com/vuka-lang/web/v2/auth", "authtest": "github.com/vuka-lang/web/v2/auth/authtest",
	"cron": "github.com/vuka-lang/web/v2/cron", "crontest": "github.com/vuka-lang/web/v2/crontest",
	"mail": "github.com/vuka-lang/web/v2/mail", "mailtest": "github.com/vuka-lang/web/v2/mail/mailtest", "graphql": "github.com/vuka-lang/web/v2/graphql",
	"orm": "github.com/vuka-lang/orm/v2", "fn": "github.com/vuka-lang/orm/v2/fn", "migration": "github.com/vuka-lang/orm/v2/migration",
	"ormtest": "github.com/vuka-lang/orm/v2/ormtest", "ormweb": "github.com/vuka-lang/orm/v2/ormweb",
	"sqlite": "github.com/vuka-lang/orm/v2/sqlite", "postgres": "github.com/vuka-lang/orm/v2/postgres", "mysql": "github.com/vuka-lang/orm/v2/mysql",
}

// parsed is a sample as Go: its declarations and the imports it names.
type parsed struct {
	decls   []decl
	imports map[string]string // name → path, as the sample writes them
	body    string            // the sample's text with its package clause and imports blanked
}

func parseSample(s sample) (*parsed, error) {
	src := s.code
	hasPkg := regexp.MustCompile(`(?m)^package \w+`).MatchString(src)
	off := 0
	if !hasPkg {
		src = "package p\n" + src
		off = 1
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("%s:%d: %v", s.page, s.line, err)
	}
	p := &parsed{imports: map[string]string{}}
	b := []byte(src)
	blank := func(from, to token.Pos) {
		for i := fset.Position(from).Offset; i < fset.Position(to).Offset; i++ {
			if b[i] != '\n' {
				b[i] = ' '
			}
		}
	}
	blank(f.Package, f.Name.End())
	for _, is := range f.Imports {
		path, _ := strconv.Unquote(is.Path.Value)
		name := path[strings.LastIndexByte(path, '/')+1:]
		if is.Name != nil {
			name = is.Name.Name
		}
		p.imports[name] = path
	}
	lineOf := func(pos token.Pos) int { return s.line + fset.Position(pos).Line - 1 - off }
	text := func(from, to token.Pos) string { return src[fset.Position(from).Offset:fset.Position(to).Offset] }
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				blank(d.Pos(), d.End())
				continue
			}
			start := d.Pos()
			if d.Doc != nil {
				start = d.Doc.Pos()
			}
			if d.Tok == token.CONST || len(d.Specs) == 1 || !d.Lparen.IsValid() {
				p.decls = append(p.decls, decl{key: genKey(d), src: text(start, d.End()), line: lineOf(start)})
				continue
			}
			for _, sp := range d.Specs {
				from := sp.Pos()
				p.decls = append(p.decls, decl{key: specKey(sp), src: d.Tok.String() + " " + text(from, sp.End()), line: lineOf(from)})
			}
		case *ast.FuncDecl:
			start := d.Pos()
			if d.Doc != nil {
				start = d.Doc.Pos()
			}
			key := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				key = recvName(d.Recv.List[0].Type) + "." + key
			}
			if key == "init" || key == "_" {
				key = ""
			}
			p.decls = append(p.decls, decl{key: key, src: text(start, d.End()), line: lineOf(start)})
		}
	}
	p.body = string(b[off*len("package p\n"):])
	return p, nil
}

func genKey(d *ast.GenDecl) string {
	var keys []string
	for _, sp := range d.Specs {
		keys = append(keys, specKey(sp))
	}
	return strings.Join(keys, ",")
}

func specKey(sp ast.Spec) string {
	switch sp := sp.(type) {
	case *ast.TypeSpec:
		return sp.Name.Name
	case *ast.ValueSpec:
		var n []string
		for _, id := range sp.Names {
			n = append(n, id.Name)
		}
		return strings.Join(n, ",")
	}
	return ""
}

func recvName(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.StarExpr:
		return recvName(e.X)
	case *ast.IndexExpr:
		return recvName(e.X)
	case *ast.IndexListExpr:
		return recvName(e.X)
	case *ast.Ident:
		return e.Name
	}
	return ""
}

// samplePackage is the package checking page[i]: the sample as a.go, and
// the declarations of the page's other samples it doesn't declare itself
// as ctx.go, the nearest sample's first.
func samplePackage(page []sample, i int) (map[string][]byte, error) {
	s := page[i]
	imports := map[string]string{}
	for k, v := range defaultImports {
		imports[k] = v
	}
	others := map[int]*parsed{}
	for j, o := range page {
		if j == i || o.fragment {
			continue
		}
		if p, err := parseSample(o); err == nil {
			others[j] = p
			for k, v := range p.imports {
				imports[k] = v
			}
		}
	}
	var main string
	own := map[string]bool{}
	if s.fragment {
		body, err := fragmentBody(s)
		if err != nil {
			return nil, err
		}
		main = body
	} else {
		p, err := parseSample(s)
		if err != nil {
			return nil, err
		}
		for k, v := range p.imports {
			imports[k] = v
		}
		for _, d := range p.decls {
			for _, k := range strings.Split(d.key, ",") {
				own[k] = true
			}
		}
		main = fmt.Sprintf("//line %s:%d\n%s\n", s.page, s.line, p.body)
	}
	var order []int
	for j := i - 1; j >= 0; j-- {
		order = append(order, j)
	}
	for j := i + 1; j < len(page); j++ {
		order = append(order, j)
	}
	var ctx strings.Builder
	taken := map[string]bool{}
	for k := range own {
		taken[k] = true
	}
	for _, j := range order {
		p := others[j]
		if p == nil {
			continue
		}
		for _, d := range p.decls {
			if d.key == "" || d.key == "main" {
				continue
			}
			clash := false
			for _, k := range strings.Split(d.key, ",") {
				clash = clash || taken[k]
			}
			if clash {
				continue
			}
			for _, k := range strings.Split(d.key, ",") {
				taken[k] = true
			}
			fmt.Fprintf(&ctx, "//line %s:%d\n%s\n\n", s.page, d.line, d.src)
		}
	}
	files := map[string][]byte{"a.go": []byte(main)}
	if ctx.Len() > 0 {
		files["ctx.go"] = []byte(ctx.String())
	}
	declared := map[string]bool{}
	for name, src := range files {
		f, err := parser.ParseFile(token.NewFileSet(), name, "package p\n"+string(src), parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					for _, k := range strings.Split(specKey(sp), ",") {
						declared[k] = true
					}
				}
			case *ast.FuncDecl:
				if d.Recv == nil {
					declared[d.Name.Name] = true
				}
			}
		}
	}
	for name, src := range files {
		files[name] = withImports(name, src, imports, declared)
	}
	return files, nil
}

// fragmentBody is a fragment sample as a function: ctx in scope, the
// variables it declares used.
func fragmentBody(s sample) (string, error) {
	head := "package p\n\nfunc _(ctx context.Context) error {\n"
	src := head + s.code + "\n}\n"
	f, err := parser.ParseFile(token.NewFileSet(), "x.go", src, parser.SkipObjectResolution)
	if err != nil {
		return "", fmt.Errorf("%s:%d: %v", s.page, s.line, err)
	}
	var used []string
	for _, st := range f.Decls[0].(*ast.FuncDecl).Body.List {
		switch st := st.(type) {
		case *ast.AssignStmt:
			if st.Tok == token.DEFINE {
				for _, l := range st.Lhs {
					if id, ok := l.(*ast.Ident); ok && id.Name != "_" {
						used = append(used, id.Name)
					}
				}
			}
		case *ast.DeclStmt:
			if g, ok := st.Decl.(*ast.GenDecl); ok && g.Tok == token.VAR {
				for _, sp := range g.Specs {
					for _, id := range sp.(*ast.ValueSpec).Names {
						if id.Name != "_" {
							used = append(used, id.Name)
						}
					}
				}
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "func _(ctx context.Context) error {\n//line %s:%d\n%s\n", s.page, s.line, s.code)
	for _, u := range used {
		fmt.Fprintf(&b, "\t_ = %s\n", u)
	}
	b.WriteString("\treturn nil\n}\n")
	return b.String(), nil
}

// withImports is a file of the package with the package clause and the
// imports of the names it uses and doesn't declare.
func withImports(name string, src []byte, imports map[string]string, declared map[string]bool) []byte {
	full := "package p\n" + string(src)
	f, err := parser.ParseFile(token.NewFileSet(), name, full, 0)
	if err != nil {
		return []byte(full)
	}
	need := map[string]bool{}
	for _, id := range f.Unresolved {
		if _, ok := imports[id.Name]; ok && !declared[id.Name] {
			need[id.Name] = true
		}
	}
	// A fragment's ctx is a parameter of type context.Context.
	if strings.Contains(full, "func _(ctx context.Context) error {") {
		need["context"] = true
	}
	var names []string
	for n := range need {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString("package p\n\nimport (\n")
	for _, n := range names {
		path := imports[n]
		if path[strings.LastIndexByte(path, '/')+1:] == n {
			fmt.Fprintf(&b, "\t%q\n", path)
		} else {
			fmt.Fprintf(&b, "\t%s %q\n", n, path)
		}
	}
	b.WriteString(")\n\n")
	b.WriteString(string(src))
	return []byte(b.String())
}

// transpileFixture resolves the same pinned libraries as the Go sample module.
func transpileFixture(t *testing.T, dir, name, code string) (*transpile.Result, error) {
	t.Helper()
	imp := importer.ForCompiler(token.NewFileSet(), "gc", func(path string) (io.ReadCloser, error) {
		cmd := exec.Command("go", "list", "-export", "-f", "{{.Export}}", path)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=readonly")
		out, err := cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("export %s: %w", path, err)
		}
		return os.Open(strings.TrimSpace(string(out)))
	})
	return transpile.Package([]transpile.File{{Name: name, Src: []byte(code)}}, transpile.Options{Importer: imp})
}
