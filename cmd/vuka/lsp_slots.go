package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
)

// Slots in the editor: inside a component's tag, <slot: completes the
// slots its source declares — a Node field, a func returning one (its let
// names that function's values), a slice of structs (one <slot:x> per
// element) — with their types; hover on a name a slot's let declares shows
// the value it takes. A slot the component has no field for, or a let on a
// slot taking no values, is the compiler's error, published as the file is
// typed.

// Slot kinds of a prop, as the compiler takes them.
const (
	slotNodeKind = 'n'
	slotFuncKind = 'f'
	slotManyKind = 'r'
)

// slotOf is how a prop of type typ takes a <slot:x>, 0 when it takes none,
// and the types of the values its content's let names.
func (p *proxy) slotOf(info *tagInfo, typ string) (byte, []string) {
	if isNodeTypeName(typ) {
		return slotNodeKind, nil
	}
	x, err := parser.ParseExpr(typ)
	if err != nil {
		return 0, nil
	}
	switch x := x.(type) {
	case *ast.FuncType:
		return funcSlot(typ, x)
	case *ast.ArrayType:
		if x.Len != nil {
			return 0, nil
		}
		fields := p.elemFields(info, x.Elt)
		var kind byte
		var params []string
		for _, fl := range fields {
			if !token.IsExported(fl.name) {
				continue
			}
			if isNodeTypeName(fl.typ) {
				kind = slotManyKind
			}
			if fx, err := parser.ParseExpr(fl.typ); err == nil {
				if ft, ok := fx.(*ast.FuncType); ok {
					if k, ps := funcSlot(fl.typ, ft); k != 0 {
						kind, params = slotManyKind, ps
					}
				}
			}
		}
		return kind, params
	}
	return 0, nil
}

// funcSlot is a func type's slot: one returning a Node, its parameters'
// types the let's.
func funcSlot(src string, ft *ast.FuncType) (byte, []string) {
	if ft.Results == nil || len(ft.Results.List) != 1 || len(ft.Results.List[0].Names) > 1 {
		return 0, nil
	}
	text := func(n ast.Node) string { return src[n.Pos()-1 : n.End()-1] }
	if !isNodeTypeName(text(ft.Results.List[0].Type)) {
		return 0, nil
	}
	var params []string
	for _, fl := range ft.Params.List {
		if _, ok := fl.Type.(*ast.Ellipsis); ok {
			return 0, nil
		}
		for range max(len(fl.Names), 1) {
			params = append(params, text(fl.Type))
		}
	}
	return slotFuncKind, params
}

// elemFields is the fields of a repeated slot's element type: a struct of
// the component's package, or of a package its file imports.
func (p *proxy) elemFields(info *tagInfo, elem ast.Expr) []tagField {
	switch x := elem.(type) {
	case *ast.IndexExpr:
		elem = x.X
	case *ast.IndexListExpr:
		elem = x.X
	}
	dir := filepath.Dir(info.file)
	name := ""
	switch x := elem.(type) {
	case *ast.Ident:
		name = x.Name
	case *ast.SelectorExpr:
		pkg, ok := x.X.(*ast.Ident)
		if !ok {
			return nil
		}
		name, dir = x.Sel.Name, ""
		for _, im := range sourceImports(info.file, p.sourceText(info.file)) {
			n := im.Name
			if n == "" {
				n = filepath.Base(tagPathMajor.ReplaceAllString(im.Path, ""))
			}
			if n == pkg.Name && !strings.HasPrefix(im.Path, ".") {
				dir = p.importDir(im.Path)
			}
		}
		if dir == "" {
			return nil
		}
	default:
		return nil
	}
	return tagPropsStruct(dir, name)
}

// slotName is the <slot:x> name of a prop: a repeated slot's is its
// field's in the singular, <slot:column> for Columns.
func slotName(pr tagProp) string {
	n := lowerCamel(pr.attr)
	if pr.kind != slotManyKind {
		return n
	}
	for _, s := range []string{"ses", "xes", "ches", "shes"} {
		if strings.HasSuffix(n, s) {
			return strings.TrimSuffix(n, "es")
		}
	}
	return strings.TrimSuffix(n, "s")
}

// fillSlots sets each prop's slot kind and let types.
func (p *proxy) fillSlots(info *tagInfo) {
	for i := range info.props {
		pr := &info.props[i]
		pr.kind, pr.params = p.slotOf(info, pr.typ)
	}
}

// requiredTag reports whether a struct tag marks its field required.
func requiredTag(tag string) bool {
	for _, v := range strings.Split(reflect.StructTag(tag).Get("vuka"), ",") {
		if strings.TrimSpace(v) == "required" {
			return true
		}
	}
	return false
}

// enclosingComponent is the component tag whose content off is in, the
// innermost; false when off is in none's.
func enclosingComponent(s *shapeScan, off int) (jsxTag, bool) {
	var open []jsxTag
	for _, t := range s.tags {
		if t.start >= off {
			break
		}
		switch {
		case t.close:
			for i := len(open) - 1; i >= 0; i-- {
				if open[i].name == t.name || baseName(open[i].name) == baseName(t.name) {
					open = open[:i]
					break
				}
			}
		case !t.self:
			open = append(open, t)
		}
	}
	if len(open) == 0 {
		return jsxTag{}, false
	}
	return open[len(open)-1], true
}

// baseName is a tag's name without its type arguments.
func baseName(tag string) string {
	if i := strings.IndexByte(tag, '['); i > 0 {
		return tag[:i]
	}
	return tag
}

// slotCompletion completes <slot: inside a component's tag with the
// component's slots; false when off is no such place.
func (p *proxy) slotCompletion(path string, src []byte, off int) ([]any, bool) {
	from := off
	for from > 0 && isNameByte(src[from-1]) {
		from--
	}
	typed := string(src[from:off])
	if from == 0 || src[from-1] != '<' || !strings.HasPrefix(typed, "slot") {
		return nil, false
	}
	s := scanShape(src)
	comp, ok := enclosingComponent(s, from-1)
	if !ok {
		return nil, false
	}
	info := p.resolveComponent(path, src, baseName(comp.name))
	if info == nil {
		return nil, false
	}
	typedRange := lspRange{positionOf(src, from), positionOf(src, off)}
	items := []any{}
	for i, pr := range info.props {
		if pr.kind == 0 {
			continue
		}
		name := slotName(pr)
		open := "slot:" + name
		if len(pr.params) > 0 {
			open += " let={${1:" + letGuess(pr.params) + "}}"
		}
		what := map[byte]string{slotNodeKind: "slot", slotFuncKind: "scoped slot", slotManyKind: "repeated slot"}[pr.kind]
		if pr.required {
			what = "required " + what
		}
		it := map[string]any{"label": "slot:" + name, "kind": 5, "detail": pr.typ, "sortText": fmt.Sprintf("%02d", i), "filterText": "slot:" + name,
			"labelDetails":     map[string]any{"detail": " " + pr.typ, "description": what},
			"textEdit":         map[string]any{"range": typedRange, "newText": open + ">$0</slot:" + name + ">"},
			"insertTextFormat": 2}
		if pr.doc != "" {
			it["documentation"] = map[string]any{"kind": "markdown", "value": pr.doc}
		}
		items = append(items, it)
	}
	return items, true
}

// letGuess is a let's names for values of types params: the first type's
// initial, then i for an int.
func letGuess(params []string) string {
	var names []string
	for i, t := range params {
		n := "v"
		if base := t[strings.LastIndexAny(t, ".*]")+1:]; base != "" {
			n = strings.ToLower(base[:1])
		}
		if i > 0 && t == "int" {
			n = "i"
		}
		for _, have := range names {
			if have == n {
				n += fmt.Sprint(i + 1)
			}
		}
		names = append(names, n)
	}
	return strings.Join(names, ", ")
}

// letNote is the hover note on a name a slot's let declares: the value of
// the slot's function it names, and its type.
func (p *proxy) letNote(path string, src []byte, r shapeRef) string {
	l := r.local
	where := "`let` of `<" + l.slot + ">`"
	info := p.resolveComponent(path, src, baseName(l.comp))
	if info == nil {
		return "**slot value** `" + r.name + "` · " + where
	}
	name := strings.TrimPrefix(l.slot, "slot:")
	for _, pr := range info.props {
		if pr.kind == 0 || slotName(pr) != name && lowerCamel(pr.attr) != name {
			continue
		}
		if l.index < len(pr.params) {
			return "**slot value** `" + r.name + " " + pr.params[l.index] + "` · " + where + ": value " + fmt.Sprint(l.index+1) + " of `" + pr.typ + "`, what `<" + l.comp + ">` passes"
		}
	}
	return "**slot value** `" + r.name + "` · " + where
}
