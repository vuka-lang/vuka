package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// enumValue is a constant of a prop's defined type: the value a quoted
// attribute writes (E0196), its name and its doc.
type enumValue struct{ value, name, doc string }

// tagEnumValues are the constants of the type of the prop a component tag's
// attribute sets, when it is a defined string type declaring some: what the
// compiler takes inside the quotes.
func (p *proxy) tagEnumValues(file string, src []byte, tag, attr string) []enumValue {
	info := p.resolveComponent(file, src, tag)
	if info == nil {
		return nil
	}
	for _, pr := range info.props {
		if strings.EqualFold(pr.attr, attr) || strings.EqualFold(pr.name, attr) {
			return p.propEnumValues(info, pr)
		}
	}
	return nil
}

// tagEnumAttrs are the attributes of a component tag setting a prop a
// quoted value completes, lowercased: ones of a defined string type
// declaring constants.
func (p *proxy) tagEnumAttrs(file string, src []byte, tag string) map[string]bool {
	info := p.resolveComponent(file, src, tag)
	if info == nil {
		return nil
	}
	var out map[string]bool
	for _, pr := range info.props {
		if len(p.propEnumValues(info, pr)) == 0 {
			continue
		}
		if out == nil {
			out = map[string]bool{}
		}
		out[strings.ToLower(pr.attr)] = true
		out[strings.ToLower(pr.name)] = true
	}
	return out
}

// tagTakesAttrs reports whether a component tag's props have an Attrs
// []ui.Attr or Attributes templ.Attributes field: where attr:name goes.
func (p *proxy) tagTakesAttrs(file string, src []byte, tag string) bool {
	info := p.resolveComponent(file, src, tag)
	if info == nil {
		return false
	}
	for _, pr := range info.props {
		switch {
		case pr.name == "Attrs" && strings.HasPrefix(pr.typ, "[]") && strings.HasSuffix(pr.typ, "Attr"),
			pr.name == "Attributes" && strings.HasSuffix(pr.typ, "templ.Attributes"):
			return true
		}
	}
	return false
}

// propEnumValues are the constants of a prop's defined string type, nil for
// any other type.
func (p *proxy) propEnumValues(info *tagInfo, pr tagProp) []enumValue {
	typ := strings.TrimPrefix(pr.typ, "*")
	if inner, ok := strings.CutPrefix(typ, "vuka.Option["); ok && strings.HasSuffix(inner, "]") {
		typ = strings.TrimSuffix(inner, "]")
	}
	qual, name := "", typ
	if i := strings.IndexByte(typ, '.'); i >= 0 {
		qual, name = typ[:i], typ[i+1:]
	}
	if !token.IsIdentifier(name) || builtinTypes[name] || strings.Contains(name, ".") {
		return nil
	}
	dir := filepath.Dir(pr.file)
	if pr.file == "" {
		dir = filepath.Dir(info.file)
	}
	if qual != "" {
		declFile := pr.file
		if declFile == "" {
			declFile = info.file
		}
		b, err := os.ReadFile(declFile)
		if err != nil {
			return nil
		}
		dir = ""
		for _, im := range sourceImports(declFile, b) {
			if im.Name == qual || im.Name == "" && path.Base(tagPathMajor.ReplaceAllString(im.Path, "")) == qual {
				dir = p.importDir(im.Path)
			}
		}
		if dir == "" {
			return nil
		}
	}
	return enumValuesIn(dir, name)
}

// enumValuesIn are the exported constants of type typeName the package at
// dir declares with a string literal value, in order.
func enumValuesIn(dir, typeName string) []enumValue {
	entries, _ := os.ReadDir(dir)
	var out []enumValue
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || strings.HasSuffix(n, "_test.go") || strings.HasSuffix(n, "_templ.go") ||
			!strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, ".vuka") && !strings.HasSuffix(n, ".vuka.html") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil || !strings.Contains(string(src), typeName) {
			continue
		}
		if strings.HasSuffix(n, ".vuka.html") {
			src = transpile.HeaderView(src)
		}
		f, _ := parser.ParseFile(token.NewFileSet(), n, src, parser.ParseComments|parser.SkipObjectResolution)
		if f == nil {
			continue
		}
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			var typ ast.Expr
			for _, s := range gd.Specs {
				vs := s.(*ast.ValueSpec)
				if vs.Type != nil || len(vs.Values) > 0 {
					typ = vs.Type
				}
				id, ok := typ.(*ast.Ident)
				if !ok || id.Name != typeName {
					continue
				}
				for j, name := range vs.Names {
					if j >= len(vs.Values) || !name.IsExported() {
						continue
					}
					lit, ok := vs.Values[j].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					value, _ := strconv.Unquote(lit.Value)
					doc := strings.TrimSpace(vs.Doc.Text() + vs.Comment.Text())
					out = append(out, enumValue{value: value, name: name.Name, doc: doc})
				}
			}
		}
	}
	return out
}

func or(s, def string) string {
	if s != "" {
		return s
	}
	return def
}
