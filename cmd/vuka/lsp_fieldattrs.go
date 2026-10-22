package main

import (
	"go/ast"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// trailingAttr is an attribute at the end of a line's text: @name, @T{…} or
// @f(…), its arguments on the line.
var trailingAttr = regexp.MustCompile(`\s@[A-Za-z0-9_.]+(?:\([^()]*\)|\{[^{}]*\})?\s*$`)

// typingAttr is the attribute name being typed at the end of a line's text.
var typingAttr = regexp.MustCompile(`\s@[A-Za-z0-9_.]*$`)

// fieldAttrSpot reports whether off is in the name of a field attribute: the
// line so far is a struct field (names and a type, or an embedded type, and
// maybe a tag), then any attributes, then @ and a (possibly qualified) name.
func fieldAttrSpot(src []byte, off int) bool {
	start := strings.LastIndexByte(string(src[:off]), '\n') + 1
	line := string(src[start:off])
	loc := typingAttr.FindStringIndex(line)
	if loc == nil {
		return false
	}
	return fieldBefore(line[:loc[0]]) && inStruct(src, start)
}

// fieldBefore reports whether text, the start of a line up to an attribute,
// is a field and its attributes.
func fieldBefore(text string) bool {
	for {
		loc := trailingAttr.FindStringIndex(text)
		if loc == nil {
			break
		}
		text = text[:loc[0]]
	}
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "@") || strings.HasPrefix(text, "//") {
		return false
	}
	c := text[0]
	if !(isIdentByte(c) || c == '*') || c >= '0' && c <= '9' {
		return false
	}
	return !strings.ContainsAny(text[len(text)-1:], "(,={")
}

// inStruct reports whether off is directly inside a struct type's braces.
// Strings and comments are skipped; an interpreted string or a rune ends at
// the end of its line at the latest, so text a markup element holds can't
// throw the count off for more than a line.
func inStruct(src []byte, off int) bool {
	var stack []bool // per open brace: does it open a struct's fields
	for i := 0; i < off; i++ {
		switch c := src[i]; c {
		case '/':
			if i+1 < off && src[i+1] == '/' {
				for i < off && src[i] != '\n' {
					i++
				}
			} else if i+1 < off && src[i+1] == '*' {
				end := strings.Index(string(src[i+2:off]), "*/")
				if end < 0 {
					return false
				}
				i += end + 3
			}
		case '`':
			end := strings.IndexByte(string(src[i+1:off]), '`')
			if end < 0 {
				return false
			}
			i += end + 1
		case '"', '\'':
			j := i + 1
			for j < off && src[j] != c && src[j] != '\n' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			if j < off && src[j] == c {
				i = j
			}
		case '{':
			before := strings.TrimRight(string(src[:i]), " \t")
			stack = append(stack, strings.HasSuffix(before, "struct") &&
				(len(before) == len("struct") || !isIdentByte(before[len(before)-len("struct")-1])))
		case '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	return len(stack) > 0 && stack[len(stack)-1]
}

// unfinishedFieldAttr is an attribute being typed on a field's line: @ alone,
// or @pkg. before another attribute. (@pkg. ending the line is a dangling
// dot, completed as one.)
var unfinishedFieldAttr = regexp.MustCompile(`(?m)\s(@)[ \t]*$|\s(@)[ \t]+@|\s@[A-Za-z_][A-Za-z0-9_]*(\.)[ \t]+@`)

// fieldPlaceholders are the places completable puts its placeholder in
// field attributes being typed: as a type's name, which a field attribute
// written bare is.
func fieldPlaceholders(src []byte) []int {
	var out []int
	for _, m := range unfinishedFieldAttr.FindAllSubmatchIndex(src, -1) {
		at := -1
		for g := 1; g <= 3; g++ {
			if m[2*g] >= 0 {
				at = m[2*g+1]
			}
		}
		if at < 0 {
			continue
		}
		start := strings.LastIndexByte(string(src[:at]), '\n') + 1
		line := string(src[start:at])
		k := strings.LastIndexByte(line, '@')
		if fieldBefore(line[:k]) && inStruct(src, start) {
			out = append(out, at)
		}
	}
	return out
}

// attrDecl is a declaration of a package that a field attribute can name.
type attrDecl struct {
	name, detail, doc, insert string
	kind                      float64
	snippet, forField         bool // forField: its type declares vuka.OnField among its targets
}

// fieldAttrDecls lists what in files a field attribute can be: a type whose
// values are data (a struct, a basic or named type, a slice, array or map;
// no interface, function, channel or pointer), not generic, whose targets
// (if it declares any) include fields; and a function returning one of
// those types. exported
// keeps only exported names: the package is another one.
func fieldAttrDecls(files []*staticsFile, exported bool) []attrDecl {
	specs := map[string]*ast.TypeSpec{}
	docs := map[string]*ast.CommentGroup{}
	targets := map[string]ast.Expr{}
	var funcs []*ast.FuncDecl
	for _, f := range files {
		for _, d := range f.ast.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				if d.Tok != token.TYPE {
					continue
				}
				for _, s := range d.Specs {
					ts := s.(*ast.TypeSpec)
					if _, dup := specs[ts.Name.Name]; dup {
						continue
					}
					specs[ts.Name.Name] = ts
					docs[ts.Name.Name] = ts.Doc
					if ts.Doc == nil && len(d.Specs) == 1 {
						docs[ts.Name.Name] = d.Doc
					}
				}
			case *ast.FuncDecl:
				if d.Recv == nil {
					funcs = append(funcs, d)
					continue
				}
				if d.Name.Name == "vukaTargets" && len(d.Recv.List) == 1 && d.Type.Results != nil && len(d.Type.Results.List) == 1 {
					if at, ok := d.Type.Results.List[0].Type.(*ast.ArrayType); ok && at.Len != nil {
						targets[typeName(d.Recv.List[0].Type)] = at.Len
					}
				}
			}
		}
	}
	visible := func(name string) bool {
		return !strings.HasPrefix(name, "__") && name != "_" && (!exported || isExported(name))
	}
	// usable says whether a type named name can be a field attribute, and
	// whether it says it is for fields.
	usable := func(name string) (ok, forField bool) {
		ts := specs[name]
		if ts == nil || ts.TypeParams != nil || ts.Assign.IsValid() {
			return false, false
		}
		if t, declared := targets[name]; declared {
			if !hasTarget(t, "OnField", 1<<3) {
				return false, false
			}
			forField = true
		}
		switch ts.Type.(type) {
		case *ast.StructType, *ast.Ident, *ast.SelectorExpr, *ast.ArrayType, *ast.MapType:
			return true, forField
		}
		return false, false
	}
	var out []attrDecl
	for name, ts := range specs {
		if !visible(name) {
			continue
		}
		ok, forField := usable(name)
		if !ok {
			continue
		}
		d := attrDecl{name: name, detail: "type " + name + " " + typeText(ts.Type), doc: docText(docs[name]), forField: forField, kind: 7}
		switch t := ts.Type.(type) {
		case *ast.StructType:
			d.kind = 22
			if !settable(t) {
				d.insert = name // @PK: the zero value
			} else {
				d.insert, d.snippet = name+"{$1}", true
			}
		case *ast.ArrayType, *ast.MapType:
			d.insert, d.snippet = name+"{$1}", true
		case *ast.Ident:
			if t.Name == "string" {
				d.insert, d.snippet = name+`("$1")`, true
			} else {
				d.insert, d.snippet = name+"($1)", true
			}
		default:
			d.insert, d.snippet = name+"($1)", true
		}
		out = append(out, d)
	}
	for _, fd := range funcs {
		name := fd.Name.Name
		if !visible(name) || fd.Type.TypeParams != nil || name == "init" || name == "main" {
			continue
		}
		res := fd.Type.Results
		if res == nil || len(res.List) != 1 || len(res.List[0].Names) > 1 {
			continue
		}
		rt, ok := res.List[0].Type.(*ast.Ident)
		if !ok {
			continue
		}
		ok, forField := usable(rt.Name)
		if !ok {
			continue
		}
		d := attrDecl{name: name, detail: "func " + name + strings.TrimPrefix(typeText(fd.Type), "func"),
			doc: docText(fd.Doc), forField: forField, kind: 3, insert: name + "($1)", snippet: true}
		if fd.Type.Params == nil || len(fd.Type.Params.List) == 0 {
			d.insert, d.snippet = name+"()", false
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// settable reports whether a struct has an exported field to set in a
// literal; one that hasn't is written bare, its zero value.
func settable(st *ast.StructType) bool {
	for _, f := range st.Fields.List {
		if len(f.Names) == 0 {
			if isExported(typeName(f.Type)) {
				return true
			}
			continue
		}
		for _, n := range f.Names {
			if isExported(n.Name) {
				return true
			}
		}
	}
	return false
}

// hasTarget reports whether a vukaTargets result's length, vuka.OnField or
// vuka.OnParam | vuka.OnField, names the target (or, as a number, has its bit).
func hasTarget(e ast.Expr, name string, bit int) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			found = found || n.Name == name
		case *ast.BasicLit:
			if n.Kind == token.INT {
				var v int
				for _, c := range n.Value {
					if c < '0' || c > '9' {
						return true
					}
					v = v*10 + int(c-'0')
				}
				found = found || v&bit != 0
			}
		}
		return true
	})
	return found
}

// typeName is the name of a receiver's or embedded field's type.
func typeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return typeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.IndexExpr:
		return typeName(t.X)
	case *ast.IndexListExpr:
		return typeName(t.X)
	}
	return ""
}

// typeText is a type for a completion's detail, a struct's fields elided as
// gopls does.
func typeText(e ast.Expr) string {
	if st, ok := e.(*ast.StructType); ok {
		if len(st.Fields.List) == 0 {
			return "struct{}"
		}
		return "struct{...}"
	}
	return exprText(e)
}

func docText(doc *ast.CommentGroup) string {
	if doc == nil {
		return ""
	}
	return strings.TrimSpace(doc.Text())
}

// fieldAttrItems are the completion items for a field attribute's name: what
// the package the qualifier names (the file's own when there is none) exports
// that a field attribute can be. ok is false when the qualifier names no
// package the file imports.
func (p *proxy) fieldAttrItems(vf *vfile, qualifier string, typed lspRange) (items []any, ok bool) {
	dir := filepath.Dir(vf.source)
	if qualifier != "" {
		dir = ""
		for _, s := range importList(vf.text()) {
			name := s.name
			if name == "" {
				name = transpile.ImportName(s.path)
			}
			if name == qualifier {
				dir = p.importDir(s.path)
				break
			}
		}
		if dir == "" {
			// A package whose name isn't its path's last element: ask its
			// files.
			for _, s := range importList(vf.text()) {
				if s.name != "" {
					continue
				}
				d := p.importDir(s.path)
				if files := p.packageFiles(d); len(files) > 0 && files[0].ast.Name.Name == qualifier {
					dir = d
					break
				}
			}
		}
		if dir == "" {
			return nil, false
		}
	}
	for _, d := range fieldAttrDecls(p.packageFiles(dir), qualifier != "") {
		rank := "1"
		if d.forField {
			rank = "0"
		}
		format := 1
		if d.snippet {
			format = 2
		}
		item := map[string]any{
			"label":            d.name,
			"kind":             d.kind,
			"detail":           d.detail,
			"sortText":         rank + d.name,
			"filterText":       d.name,
			"textEdit":         map[string]any{"range": typed, "newText": d.insert},
			"insertTextFormat": format,
		}
		if d.doc != "" {
			item["documentation"] = map[string]any{"kind": "markdown", "value": d.doc}
		}
		items = append(items, item)
	}
	return items, true
}

// withFieldAttrItems is a field attribute's completion: the attribute items,
// and when nothing is qualified yet, the packages gopls offers (an attribute
// of another package starts with its name).
func withFieldAttrItems(v any, items []any, qualifier string, typed lspRange) any {
	var kept []any
	if qualifier == "" {
		existing, _ := v.([]any)
		if list, ok := v.(map[string]any); ok {
			existing, _ = list["items"].([]any)
		}
		for _, it := range existing {
			if m, ok := it.(map[string]any); ok && m["kind"] == float64(9) {
				if te, ok := m["textEdit"].(map[string]any); ok {
					delete(te, "insert")
					delete(te, "replace")
					te["range"] = typed
				}
				kept = append(kept, m)
			}
		}
	}
	return map[string]any{"isIncomplete": false, "items": append(kept, items...)}
}
