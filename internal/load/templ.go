package load

import (
	"bytes"
	"go/format"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/transpile"
)

// TemplPath is the import path of templ's runtime, which the Go generated
// for a .templ file imports.
const TemplPath = "github.com/a-h/templ"

// Templ is a .templ file of a package and the Go templ's generator makes of it.
type Templ struct {
	Name   string // base name: "page.templ"
	Src    []byte
	GoName string // TemplGoName(Name)
	Go     []byte // templ's raw output; nil when Err is set
	// Map is templ's source map between Src and Go (line and column,
	// 0-based): a *parser.SourceMap of templ's, from TemplCompiler.
	Map any
	Err *transpile.Error // a parse or generate error, positioned in Src
	// Live is Go instrumented to record live render trees (TemplInstrument),
	// gofmt'ed: what builds use in place of Go, when the module's ui has
	// the recorder (TemplTrees). The language server keeps to Go and Map.
	Live []byte

	live bool // instrumented when compiled
}

// scopeTempl compiles again, with their elements marked, the .templ files
// of p declaring a component scopes names.
func (p *Package) scopeTempl(scopes map[string]string) {
	for _, t := range p.Templ {
		if t.Err != nil || !declaresTempl(t.Src, scopes) {
			continue
		}
		*t = *compileTempl(filepath.Join(p.Dir, t.Name), t.Name, t.Src, t.live, scopes)
		for i, f := range p.Files {
			if f.Name == t.GoName && t.Err == nil {
				p.Files[i].Src = t.Go
			}
		}
	}
}

var templDecl = regexp.MustCompile(`(?m)^templ\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)\s*[\[(]`)

func declaresTempl(src []byte, names map[string]string) bool {
	for _, m := range templDecl.FindAllSubmatch(src, -1) {
		if _, ok := names[string(m[1])]; ok {
			return true
		}
	}
	return false
}

// TemplGoName is the name of the Go file templ generates for a .templ file.
func TemplGoName(name string) string { return strings.TrimSuffix(name, ".templ") + "_templ.go" }

func isTempl(name string) bool { return strings.HasSuffix(name, ".templ") }

// TemplCompiler compiles a .templ file as `templ generate` would, without its
// formatting pass, returning the Go and templ's source map. The vuka
// command's templ plugin sets it: the plugin links templ's parser and
// generator, the language module doesn't. Nil leaves .templ files alone.
// scopes names the file's scoped components (Options.Scopes): every element
// one writes carries data-vk-s="<id>".
var TemplCompiler func(path, name string, src []byte, scopes map[string]string) (goSrc []byte, sourceMap any, err *transpile.Error)

// UIPath is the JSX target whose templx package registers a .templ file's
// components for a vuka.File naming it.
const UIPath = "github.com/vuka-lang/ui"

// TemplInstrument rewrites a .templ file's Go to record live render trees
// (ui.Rec). The vuka command's templ plugin sets it.
var TemplInstrument func(goSrc []byte) ([]byte, error)

// compileTempl compiles one .templ file with TemplCompiler, and instruments
// it when live.
func compileTempl(path, name string, src []byte, live bool, scopes map[string]string) *Templ {
	t := &Templ{Name: name, Src: src, GoName: TemplGoName(name), live: live}
	t.Go, t.Map, t.Err = TemplCompiler(path, name, src, scopes)
	if t.Err == nil && live && TemplInstrument != nil {
		out, err := TemplInstrument(t.Go)
		if err != nil {
			t.Err = &transpile.Error{Pos: token.Position{Filename: path, Line: 1, Column: 1}, Msg: "instrumenting the generated Go: " + err.Error()}
			return t
		}
		t.Live = out
	}
	return t
}

// Build is t's Go as builds use it: instrumented when it is, else as `templ
// generate` writes it.
func (t *Templ) Build() []byte {
	if t.Live != nil {
		return t.Live
	}
	return t.formatted()
}

var requireLine = regexp.MustCompile(`(?m)^\s*(?:require\s+)?(\S+)\s+(v\S+)`)

// requires reports whether the go.mod in root requires each of paths.
func requires(root string, paths ...string) bool {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return false
	}
	have := map[string]bool{}
	for _, m := range requireLine.FindAllSubmatch(data, -1) {
		have[string(m[1])] = true
	}
	for _, p := range paths {
		if !have[p] {
			return false
		}
	}
	return true
}

// TemplActive reports whether .templ files of the module at root are
// compiled: the templ plugin is linked and the module requires templ.
func TemplActive(root string) bool { return TemplCompiler != nil && requires(root, TemplPath) }

// TemplRegistry is the package the code generated for a vuka.File naming a
// .templ file registers its components with (transpile.Options.TemplRegistry):
// github.com/vuka-lang/ui/templx when the module at root compiles .templ
// files and requires ui; else "".
func TemplRegistry(root string) string {
	if TemplActive(root) && requires(root, UIPath) {
		return UIPath + "/templx"
	}
	return ""
}

// TemplTrees reports whether the .templ files of the module at root are
// instrumented to record live render trees: they are compiled, the module
// requires ui (or is ui), and that ui has the recorder (templx.Record) — v0.5.0
// or later, or a local replacement that has it.
func TemplTrees(root string) bool {
	if !TemplActive(root) || TemplInstrument == nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return false
	}
	if m := modulePath.FindSubmatch(data); m != nil && string(m[1]) == UIPath {
		return true
	}
	version := ""
	for _, m := range requireLine.FindAllSubmatch(data, -1) {
		if string(m[1]) == UIPath {
			version = string(m[2])
		}
	}
	if version == "" {
		return false
	}
	if m := uiReplace.FindSubmatch(data); m != nil {
		to := string(m[1])
		if strings.HasPrefix(to, ".") || filepath.IsAbs(to) {
			if !filepath.IsAbs(to) {
				to = filepath.Join(root, to)
			}
			rec, err := os.ReadFile(filepath.Join(to, "templx", "record.go"))
			return err == nil && bytes.Contains(rec, []byte("func Record("))
		}
		if v := strings.Fields(to); len(v) == 2 && v[0] == UIPath {
			version = v[1]
		}
	}
	return atLeast(version, 0, 5)
}

var (
	modulePath = regexp.MustCompile(`(?m)^\s*module\s+(\S+)`)
	uiReplace  = regexp.MustCompile(`(?m)^\s*(?:replace\s+)?` + regexp.QuoteMeta(UIPath) + `(?:\s+v\S+)?\s+=>\s+(.+?)\s*$`)
)

// atLeast reports whether version vMAJOR.MINOR… is major.minor or later.
func atLeast(version string, major, minor int) bool {
	f := strings.SplitN(strings.TrimPrefix(version, "v"), ".", 3)
	if len(f) < 2 {
		return false
	}
	ma, err1 := strconv.Atoi(f[0])
	mi, err2 := strconv.Atoi(f[1])
	return err1 == nil && err2 == nil && (ma > major || ma == major && mi >= minor)
}

// formatted is t's Go as `templ generate` writes it: gofmt'ed.
func (t *Templ) formatted() []byte {
	if out, err := format.Source(t.Go); err == nil {
		return out
	}
	return t.Go
}

var templPackage = regexp.MustCompile(`(?m)^package\s+([A-Za-z_]\w*)`)

// templPackageName is the package a .templ file declares, read from its
// source when templ can't parse it.
func templPackageName(src []byte) string {
	if g := templPackage.FindSubmatch(src); g != nil {
		return string(g[1])
	}
	return ""
}
