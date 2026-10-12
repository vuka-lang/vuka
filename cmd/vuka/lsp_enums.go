package main

// Completion and hover after an enum's name: Status.Draft, Status.Values,
// Status.Parse, Shape.Circle. An enum lowers to Go named like hand-written Go
// enums (StatusDraft, StatusValues, ParseStatus, ShapeCircle), not Type_Member
// statics, so they are found by the shapes the transpiler generates: a value
// enum is a type T with a func TValues, its variants T's constants named
// T<Variant>; an enum with data is an interface T{ isT() }, its variants the
// types with an isT method.

import (
	"go/ast"
	"go/token"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// enumDecls are the members of typeName when it is an enum, else nil.
func enumDecls(files []*staticsFile, typeName string, samePkg bool, written string) []staticMember {
	visible := func(name string) bool { return samePkg || isExported(name) }
	upper := func(s string) string {
		r, n := utf8.DecodeRuneInString(s)
		return string(unicode.ToUpper(r)) + s[n:]
	}
	parse := "parse" + upper(typeName)
	if isExported(typeName) {
		parse = "Parse" + typeName
	}
	seal := "is" + upper(typeName)
	variant := func(goName string) (string, bool) {
		v, ok := strings.CutPrefix(goName, typeName)
		if !ok || v == "" {
			return "", false
		}
		r, _ := utf8.DecodeRuneInString(v)
		return v, unicode.IsUpper(r)
	}

	typeWord := regexp.MustCompile(`\b` + regexp.QuoteMeta(typeName) + `\b`)
	qualify := func(text string) string { return typeWord.ReplaceAllString(text, written) }
	var out []staticMember
	sum, values := false, false
	funcs := map[string]*ast.FuncDecl{}
	sealed := map[string]bool{} // types with the seal method
	for _, f := range files {
		for _, d := range f.ast.Decls {
			switch decl := d.(type) {
			case *ast.FuncDecl:
				if decl.Recv == nil {
					funcs[decl.Name.Name] = decl
					values = values || decl.Name.Name == typeName+"Values"
				} else if decl.Name.Name == seal && len(decl.Recv.List) == 1 {
					if id, ok := decl.Recv.List[0].Type.(*ast.Ident); ok {
						sealed[id.Name] = true
					}
				}
			case *ast.GenDecl:
				for _, s := range decl.Specs {
					ts, ok := s.(*ast.TypeSpec)
					if !ok || ts.Name.Name != typeName {
						continue
					}
					if it, ok := ts.Type.(*ast.InterfaceType); ok && len(it.Methods.List) == 1 && len(it.Methods.List[0].Names) == 1 && it.Methods.List[0].Names[0].Name == seal {
						sum = true
					}
				}
			}
		}
	}
	switch {
	case sum:
		for _, f := range files {
			for _, d := range f.ast.Decls {
				g, ok := d.(*ast.GenDecl)
				if !ok || g.Tok != token.TYPE {
					continue
				}
				for _, s := range g.Specs {
					ts := s.(*ast.TypeSpec)
					v, ok := variant(ts.Name.Name)
					if !ok || !sealed[ts.Name.Name] || !visible(ts.Name.Name) {
						continue
					}
					doc := ts.Doc
					if doc == nil && len(g.Specs) == 1 {
						doc = g.Doc
					}
					detail := "static " + v + " " + qualify(exprText(ts.Type))
					out = append(out, staticMember{name: v, kind: 20, static: true, detail: detail,
						doc: staticDocText(doc, ts.Name.Name, written+"."+v)})
				}
			}
		}
	case values:
		for _, f := range files {
			for _, d := range f.ast.Decls {
				g, ok := d.(*ast.GenDecl)
				if !ok || g.Tok != token.CONST {
					continue
				}
				typ := ""
				for _, s := range g.Specs {
					vs := s.(*ast.ValueSpec)
					switch {
					case vs.Type != nil:
						typ = exprText(vs.Type)
					case len(vs.Values) > 0:
						typ = ""
					}
					if typ != typeName {
						continue
					}
					for _, n := range vs.Names {
						v, ok := variant(n.Name)
						if !ok || !visible(n.Name) {
							continue
						}
						out = append(out, staticMember{name: v, kind: 20, static: true,
							detail: "static const " + v + " " + written,
							doc:    staticDocText(vs.Doc, n.Name, written+"."+v)})
					}
				}
			}
		}
		for _, m := range [][2]string{{"Values", typeName + "Values"}, {"Parse", parse}} {
			member, goName := m[0], m[1]
			if fd := funcs[goName]; fd != nil && visible(goName) {
				out = append(out, staticMember{name: member, kind: 3, static: true,
					detail: "static " + qualify(exprText(fd.Type)),
					doc:    staticDocText(fd.Doc, goName, written+"."+member)})
			}
		}
	}
	return out
}
