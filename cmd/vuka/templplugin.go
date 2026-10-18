package main

import (
	"bytes"
	"errors"
	"go/token"
	"slices"
	"strings"

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
// The elements of the components scopes names get their scope's attribute.
func compileTempl(path, name string, src []byte, scopes map[string]string) ([]byte, any, *transpile.Error) {
	fail := func(err error) ([]byte, any, *transpile.Error) {
		var pe parse.ParseError
		pos := parse.Position{}
		msg := err.Error()
		if errors.As(err, &pe) {
			pos, msg = pe.Pos, pe.Msg
		}
		return nil, nil, &transpile.Error{Pos: token.Position{Filename: path, Offset: pos.Index, Line: pos.Line + 1, Column: pos.Col + 1}, Msg: msg, Code: "E0006"}
	}
	tf, err := templparser.ParseString(string(src))
	if err != nil {
		return fail(err)
	}
	tf.Filepath = path
	markTemplScopes(tf, scopes)
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

// markTemplScopes adds data-vk-s="<id>" to every element a scoped
// component writes: in its body, its branches and loops, and the children
// it passes to the components it calls, but not inside those components,
// whose markup is their own. A constant attribute, it is a static of the
// component's live render tree.
func markTemplScopes(tf *templparser.TemplateFile, scopes map[string]string) {
	if len(scopes) == 0 {
		return
	}
	for _, n := range tf.Nodes {
		t, ok := n.(*templparser.HTMLTemplate)
		if !ok {
			continue
		}
		if id, ok := scopes[templName(t.Expression.Value)]; ok {
			markTemplNodes(t.Children, id)
		}
	}
}

// templName is the name in a templ component's signature: Layout in
// "Layout(title string)", Show in "(p Page) Show()".
func templName(sig string) string {
	sig = strings.TrimSpace(sig)
	if strings.HasPrefix(sig, "(") {
		if i := strings.IndexByte(sig, ')'); i >= 0 {
			sig = strings.TrimSpace(sig[i+1:])
		}
	}
	if i := strings.IndexAny(sig, "([ "); i >= 0 {
		sig = sig[:i]
	}
	return sig
}

func markTemplNodes(ns []templparser.Node, id string) {
	for _, n := range ns {
		switch n := n.(type) {
		case *templparser.Element:
			if !slices.ContainsFunc(n.Attributes, func(a templparser.Attribute) bool {
				c, ok := a.(*templparser.ConstantAttribute)
				return ok && c.Key.String() == scopeAttr
			}) {
				n.Attributes = append(n.Attributes, &templparser.ConstantAttribute{Key: templparser.ConstantAttributeKey{Name: scopeAttr}, Value: id})
			}
			markTemplNodes(n.Children, id)
		case *templparser.IfExpression:
			markTemplNodes(n.Then, id)
			for _, e := range n.ElseIfs {
				markTemplNodes(e.Then, id)
			}
			markTemplNodes(n.Else, id)
		case *templparser.SwitchExpression:
			for _, c := range n.Cases {
				markTemplNodes(c.Children, id)
			}
		case *templparser.ForExpression:
			markTemplNodes(n.Children, id)
		case *templparser.TemplElementExpression:
			markTemplNodes(n.Children, id)
		}
	}
}
