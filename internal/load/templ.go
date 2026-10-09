package load

import (
	"bytes"
	"errors"
	"go/format"
	"go/token"
	"regexp"
	"strings"

	"github.com/a-h/parse"
	"github.com/a-h/templ"
	"github.com/a-h/templ/generator"
	templparser "github.com/a-h/templ/parser/v2"

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
	// Map is templ's source map between Src and Go (line and column, 0-based).
	Map *templparser.SourceMap
	Err *transpile.Error // a parse or generate error, positioned in Src
}

// TemplGoName is the name of the Go file templ generates for a .templ file.
func TemplGoName(name string) string { return strings.TrimSuffix(name, ".templ") + "_templ.go" }

func isTempl(name string) bool { return strings.HasSuffix(name, ".templ") }

// compileTempl runs templ's parser and generator on one file, as `templ
// generate` would, without its formatting pass, so Map matches Go.
func compileTempl(path, name string, src []byte) *Templ {
	t := &Templ{Name: name, Src: src, GoName: TemplGoName(name)}
	fail := func(pos parse.Position, msg string) *Templ {
		t.Err = &transpile.Error{Pos: token.Position{Filename: path, Offset: pos.Index, Line: pos.Line + 1, Column: pos.Col + 1}, Msg: msg}
		return t
	}
	tf, err := templparser.ParseString(string(src))
	if err != nil {
		var pe parse.ParseError
		if errors.As(err, &pe) {
			return fail(pe.Pos, pe.Msg)
		}
		return fail(parse.Position{}, err.Error())
	}
	tf.Filepath = path
	var buf bytes.Buffer
	out, err := generator.Generate(tf, &buf, generator.WithFileName(name), generator.WithVersion(templ.Version()))
	if err != nil {
		var pe parse.ParseError
		if errors.As(err, &pe) {
			return fail(pe.Pos, pe.Msg)
		}
		return fail(parse.Position{}, err.Error())
	}
	t.Go, t.Map = buf.Bytes(), out.SourceMap
	return t
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
