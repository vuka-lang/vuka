package main

import (
	"bytes"
	"errors"
	"go/token"

	"github.com/a-h/parse"
	"github.com/a-h/templ"
	"github.com/a-h/templ/generator"
	templparser "github.com/a-h/templ/parser/v2"

	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

// The templ plugin: .templ files of a module that requires templ compile with
// templ's own parser and generator, linked into the vuka command (the tool),
// not the language's runtime.
func init() { load.TemplCompiler = compileTempl }

// compileTempl runs templ's parser and generator on one file, as `templ
// generate` would, without its formatting pass, so the source map matches Go.
func compileTempl(path, name string, src []byte) ([]byte, any, *transpile.Error) {
	fail := func(err error) ([]byte, any, *transpile.Error) {
		var pe parse.ParseError
		pos := parse.Position{}
		msg := err.Error()
		if errors.As(err, &pe) {
			pos, msg = pe.Pos, pe.Msg
		}
		return nil, nil, &transpile.Error{Pos: token.Position{Filename: path, Offset: pos.Index, Line: pos.Line + 1, Column: pos.Col + 1}, Msg: msg}
	}
	tf, err := templparser.ParseString(string(src))
	if err != nil {
		return fail(err)
	}
	tf.Filepath = path
	var buf bytes.Buffer
	out, err := generator.Generate(tf, &buf, generator.WithFileName(name), generator.WithVersion(templ.Version()))
	if err != nil {
		return fail(err)
	}
	return buf.Bytes(), out.SourceMap, nil
}

// templRegistry is load.TemplRegistry for the module enclosing dir.
func templRegistry(dir string) string {
	root, _, err := load.ModuleRoot(dir)
	if err != nil {
		return ""
	}
	return load.TemplRegistry(root)
}
