package load

import (
	"go/format"
	"os"
	"path/filepath"
	"regexp"
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
}

// TemplGoName is the name of the Go file templ generates for a .templ file.
func TemplGoName(name string) string { return strings.TrimSuffix(name, ".templ") + "_templ.go" }

func isTempl(name string) bool { return strings.HasSuffix(name, ".templ") }

// TemplCompiler compiles a .templ file as `templ generate` would, without its
// formatting pass, returning the Go and templ's source map. The vuka
// command's templ plugin sets it: the plugin links templ's parser and
// generator, the language module doesn't. Nil leaves .templ files alone.
var TemplCompiler func(path, name string, src []byte) (goSrc []byte, sourceMap any, err *transpile.Error)

// UIPath is the JSX target whose templx package registers a .templ file's
// components for a vuka.File naming it.
const UIPath = "github.com/vuka-lang/ui"

// compileTempl compiles one .templ file with TemplCompiler.
func compileTempl(path, name string, src []byte) *Templ {
	t := &Templ{Name: name, Src: src, GoName: TemplGoName(name)}
	t.Go, t.Map, t.Err = TemplCompiler(path, name, src)
	return t
}

var requireLine = regexp.MustCompile(`(?m)^\s*(?:require\s+)?(\S+)\s+v\S+`)

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
