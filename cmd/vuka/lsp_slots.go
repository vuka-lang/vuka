package main

import (
	"bytes"
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
// element) — with their types and docs, but those the tag already fills;
// hover on a slot's tag shows its field, its doc and its let's types, and
// definition goes to the field; hover on a name a slot's let declares shows
// the value it takes. A repeated slot's tag completes its element's fields
// and let, never an element's attributes, and its content completes the
// element's slots. A slot the component has no field for (E0106, with
// the nearest one's name), or a let on a slot taking no values, is the
// compiler's error, published as the file is typed.

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
		if !ok || p == nil {
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
	s := scanShape(path, src)
	sc, ok := p.slotScope(path, src, s, from-1, "")
	if !ok {
		return nil, false
	}
	used := usedSlots(src, s, sc.comp, sc.from, from-1)
	for _, a := range sc.inTag.attrs {
		used[lowerCamel(a.name)] = true // a field its element's tag sets
	}
	typedRange := lspRange{positionOf(src, from), positionOf(src, off)}
	items := []any{}
	for i, pr := range sc.props {
		if pr.kind == 0 || pr.kind != slotManyKind && used[slotName(pr)] {
			continue
		}
		name := slotName(pr)
		open := "slot:" + name
		if len(pr.params) > 0 {
			open += " let={${1:" + letGuess(pr.params) + "}}"
		}
		what := slotWhat(pr)
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

// slotWhat is the kind of slot pr is, as completion and hover say it.
func slotWhat(pr tagProp) string {
	what := map[byte]string{slotNodeKind: "slot", slotFuncKind: "scoped slot", slotManyKind: "repeated slot"}[pr.kind]
	if pr.required {
		what = "required " + what
	}
	return what
}

// usedSlots are the slots filled by the content starting at from, comp's
// or one of its slot's, but the one whose tag is at skip: its own
// <slot:x> tags, not those inside them or another component.
func usedSlots(src []byte, s *shapeScan, comp jsxTag, from, skip int) map[string]bool {
	used := map[string]bool{}
	depth := 0
	for i := from; i < len(src); i++ {
		if src[i] != '<' || i == skip || !slotTagAt(src, i) {
			continue
		}
		in, ok := enclosingComponent(s, i)
		if !ok || in.start < comp.start {
			break
		}
		t, ok := readTag(src, i)
		if !ok || in.start != comp.start {
			continue
		}
		if t.close {
			if depth == 0 {
				break
			}
			depth--
			continue
		}
		if depth == 0 {
			used[strings.TrimPrefix(t.name, "slot:")] = true
		}
		if !t.self {
			depth++
		}
	}
	return used
}

// elemAttrs are the fields of a repeated slot's element its tag sets as
// attributes: all but its func fields, which its content fills. A Node
// field is both, header="Name" or <slot:header>.
func elemAttrs(props []tagProp) []tagProp {
	var out []tagProp
	for _, ep := range props {
		if ep.kind != slotFuncKind {
			out = append(out, ep)
		}
	}
	return out
}

// slotTagProp is the prop the slot tag t fills, the scope it is in, and the
// element attributes its tag takes.
func (p *proxy) slotTagProp(path string, src []byte, s *shapeScan, t jsxTag) (tagProp, slotOwner, []tagProp, bool) {
	sc, ok := p.slotScope(path, src, s, t.start, "")
	if !ok {
		return tagProp{}, sc, nil, false
	}
	pr, ok := slotPropIn(sc.props, strings.TrimPrefix(t.name, "slot:"))
	if !ok || pr.kind == 0 {
		return tagProp{}, sc, nil, false
	}
	var attrs []tagProp
	if pr.kind == slotManyKind {
		attrs = elemAttrs(p.elemProps(sc.info, pr))
	}
	return pr, sc, attrs, true
}

// slotAttrCompletion completes the attributes in a slot's tag: a repeated
// slot's element fields, and let for a slot passing values; never an
// element's attributes. false when off is in no slot's tag.
func (p *proxy) slotAttrCompletion(path string, src []byte, off int) ([]any, bool) {
	spot, ok := completionSpot(src, off)
	if !ok || spot.kind != "attr" || !strings.HasPrefix(spot.tag.name, "slot:") {
		return nil, false
	}
	items := []any{}
	pr, _, attrs, ok := p.slotTagProp(path, src, scanShape(path, src), spot.tag)
	if !ok {
		return items, true
	}
	used := map[string]bool{}
	for _, a := range spot.tag.attrs {
		if off < a.start || off > a.end {
			used[strings.ToLower(a.name)] = true
		}
	}
	typed := lspRange{positionOf(src, spot.from), positionOf(src, off)}
	for i, ep := range attrs {
		if used[strings.ToLower(ep.attr)] {
			continue
		}
		text, format := ep.attr+"={$1}", 2
		switch {
		case ep.typ == "bool":
			text, format = ep.attr, 1
		case ep.typ == "string" || ep.kind == slotNodeKind:
			text = ep.attr + `="$1"`
		}
		it := map[string]any{"label": ep.attr, "kind": 5, "detail": ep.typ, "sortText": fmt.Sprintf("1%02d", i), "filterText": ep.attr,
			"labelDetails": map[string]any{"detail": " " + ep.typ},
			"textEdit":     map[string]any{"range": typed, "newText": text}, "insertTextFormat": format}
		if ep.doc != "" {
			it["documentation"] = map[string]any{"kind": "markdown", "value": ep.doc}
		}
		items = append(items, it)
	}
	if len(pr.params) > 0 && !used["let"] {
		items = append(items, map[string]any{"label": "let", "kind": 14, "detail": "names " + strings.Join(pr.params, ", "), "sortText": "000", "filterText": "let",
			"labelDetails":     map[string]any{"detail": " " + strings.Join(pr.params, ", ")},
			"documentation":    map[string]any{"kind": "markdown", "value": "Names the values the slot's content takes, in order: `" + strings.Join(pr.params, "`, `") + "`. Name fewer to leave the rest out."},
			"textEdit":         map[string]any{"range": typed, "newText": "let={${1:" + letGuess(pr.params) + "}}"},
			"insertTextFormat": 2})
	}
	return items, true
}

// slotAttrHover is the hover on an attribute's name in a slot's tag: the
// element field it sets, or the values let names.
func (p *proxy) slotAttrHover(path string, src []byte, s *shapeScan, off int) any {
	t, ok := tagAround(src, off)
	if !ok || !strings.HasPrefix(t.name, "slot:") {
		return nil
	}
	for _, a := range t.attrs {
		if off < a.start || off > a.end {
			continue
		}
		pr, sc, attrs, ok := p.slotTagProp(path, src, s, t)
		if !ok {
			return nil
		}
		if a.name == "let" {
			if len(pr.params) == 0 {
				return nil
			}
			return markdownHover("**let** of `<"+t.name+">` · names the values its content takes, in order: `"+strings.Join(pr.params, "`, `")+"`, of `"+p.letFunc(sc.info, pr)+"`; name fewer to leave the rest out", src, a.start, a.end)
		}
		for _, ep := range attrs {
			if strings.EqualFold(ep.attr, a.name) {
				md := "**field** `" + ep.name + " " + ep.typ + "` of `<" + t.name + ">`'s element"
				if ep.file != "" {
					md += fmt.Sprintf(" · [%s](%s#L%d)", filepath.Base(ep.file), pathToURI(ep.file), ep.line)
				}
				if ep.doc != "" {
					md += "\n\n" + ep.doc
				}
				return markdownHover(md, src, a.start, a.end)
			}
		}
	}
	return nil
}

// slotTagAt reports whether a slot's tag, opening or closing, starts at i.
func slotTagAt(src []byte, i int) bool {
	return bytes.HasPrefix(src[i:], []byte("<slot:")) || bytes.HasPrefix(src[i:], []byte("</slot:"))
}

// openSlots are comp's slot tags open at off, outermost first.
func openSlots(src []byte, s *shapeScan, comp jsxTag, off int) []jsxTag {
	var open []jsxTag
	for i := comp.end; i < off && i < len(src); i++ {
		if src[i] != '<' || !slotTagAt(src, i) {
			continue
		}
		if in, ok := enclosingComponent(s, i); !ok || in.start != comp.start {
			continue
		}
		t, ok := readTag(src, i)
		switch {
		case !ok:
		case t.close && len(open) > 0:
			open = open[:len(open)-1]
		case !t.close && !t.self:
			open = append(open, t)
		}
	}
	return open
}

// slotOwner is what a slot written at off fills: the slots of a component,
// or, inside one of its repeated slots, those of that slot's element.
type slotOwner struct {
	comp  jsxTag    // the component
	info  *tagInfo  // its declaration
	owner string    // the tag whose slots they are
	in    *tagProp  // the repeated slot, inside one
	inTag jsxTag    // its tag
	props []tagProp // the slots, and an element's other fields
	from  int       // where the owner's content starts
}

// slotScope finds what a slot written at off fills; close names the slot a
// closing tag at off ends, "" when none does. false when off is in no
// component's content, or in a slot's that takes no slots.
func (p *proxy) slotScope(path string, src []byte, s *shapeScan, off int, close string) (slotOwner, bool) {
	comp, ok := enclosingComponent(s, off)
	if !ok {
		return slotOwner{}, false
	}
	info := p.resolveComponent(path, src, baseName(comp.name))
	if info == nil {
		return slotOwner{}, false
	}
	open := openSlots(src, s, comp, off)
	if n := len(open); close != "" && n > 0 && open[n-1].name == close {
		open = open[:n-1]
	}
	sc := slotOwner{comp: comp, info: info, owner: comp.name, props: info.props, from: comp.end}
	switch len(open) {
	case 0:
		return sc, true
	case 1:
		pr, ok := info.slotProp(strings.TrimPrefix(open[0].name, "slot:"))
		if !ok || pr.kind != slotManyKind {
			return slotOwner{}, false
		}
		sc.owner, sc.in, sc.inTag, sc.props, sc.from = open[0].name, &pr, open[0], p.elemProps(info, pr), open[0].end
		return sc, true
	}
	return slotOwner{}, false
}

// elemProps are the exported fields of a repeated slot's element struct:
// its Node and func fields slots, the others attributes.
func (p *proxy) elemProps(info *tagInfo, pr tagProp) []tagProp {
	x, err := parser.ParseExpr(pr.typ)
	at, ok := x.(*ast.ArrayType)
	if err != nil || !ok {
		return nil
	}
	var out []tagProp
	for _, fl := range p.elemFields(info, at.Elt) {
		if !token.IsExported(fl.name) {
			continue
		}
		ep := tagProp{attr: lowerCamel(fl.name), typ: fl.typ, name: fl.name, file: fl.file, line: fl.line, doc: fl.doc, required: requiredTag(fl.tag)}
		if isNodeTypeName(fl.typ) {
			ep.kind = slotNodeKind
		} else if fx, err := parser.ParseExpr(fl.typ); err == nil {
			if ft, ok := fx.(*ast.FuncType); ok {
				ep.kind, ep.params = funcSlot(fl.typ, ft)
			}
		}
		out = append(out, ep)
	}
	return out
}

// slotProp is the prop a <slot:name> fills.
func (info *tagInfo) slotProp(name string) (tagProp, bool) { return slotPropIn(info.props, name) }

// slotPropIn is the prop of props a <slot:name> fills.
func slotPropIn(props []tagProp, name string) (tagProp, bool) {
	for _, pr := range props {
		if pr.kind != 0 && (slotName(pr) == name || lowerCamel(pr.attr) == name) {
			return pr, true
		}
	}
	return tagProp{}, false
}

// slotAt is the slot tag whose name holds off, opening or closing, what it
// is a slot of and the prop it fills.
func (p *proxy) slotAt(path string, src []byte, s *shapeScan, off int) (jsxTag, slotOwner, tagProp, bool) {
	i := bytes.LastIndexByte(src[:min(off+1, len(src))], '<')
	if i < 0 {
		return jsxTag{}, slotOwner{}, tagProp{}, false
	}
	t, ok := readTag(src, i)
	if !ok || !strings.HasPrefix(t.name, "slot:") || off < t.nameStart || off > t.nameEnd {
		return jsxTag{}, slotOwner{}, tagProp{}, false
	}
	closing := ""
	if t.close {
		closing = t.name
	}
	sc, ok := p.slotScope(path, src, s, i, closing)
	if !ok {
		return jsxTag{}, slotOwner{}, tagProp{}, false
	}
	pr, ok := slotPropIn(sc.props, strings.TrimPrefix(t.name, "slot:"))
	return t, sc, pr, ok && pr.kind != 0
}

// slotHover is the hover on a slot tag's name: the field it fills, its doc,
// and the types of the values its let names, inferred type arguments
// substituted.
func (p *proxy) slotHover(path string, src []byte, s *shapeScan, off int) any {
	t, sc, pr, ok := p.slotAt(path, src, s, off)
	if !ok {
		return nil
	}
	var b strings.Builder
	field := pr.name
	if field == "" {
		field = pr.attr
	}
	fmt.Fprintf(&b, "**%s** `<%s>` · `%s %s` of `<%s>`", slotWhat(pr), t.name, field, pr.typ, sc.owner)
	if pr.file != "" {
		fmt.Fprintf(&b, " · [%s](%s#L%d)", filepath.Base(pr.file), pathToURI(pr.file), pr.line)
	}
	if pr.kind == slotManyKind {
		b.WriteString("\n\nEach `<" + t.name + ">` is one element")
		if attrs := elemAttrs(p.elemProps(sc.info, pr)); len(attrs) > 0 {
			names := make([]string, len(attrs))
			for i, a := range attrs {
				names[i] = a.attr
			}
			b.WriteString(", its attributes `" + strings.Join(names, "`, `") + "`")
		}
		b.WriteString(".")
	}
	if pr.doc != "" {
		b.WriteString("\n\n" + pr.doc)
	}
	if len(pr.params) > 0 {
		names, offs := letNames(src, t)
		if len(names) == 0 {
			names = strings.Split(letGuess(pr.params), ", ")
		}
		var vals []string
		for i, typ := range pr.params {
			n := "_"
			if i < len(names) {
				n = names[i]
			}
			if i < len(offs) {
				if inferred := p.letType(path, offs[i]); inferred != "" {
					typ = inferred
				}
			}
			vals = append(vals, "`"+n+" "+typ+"`")
		}
		b.WriteString("\n\n`let={" + strings.Join(names, ", ") + "}` names " + strings.Join(vals, ", "))
	}
	return markdownHover(b.String(), src, t.nameStart, t.nameEnd)
}

// letNames are the names a slot tag's let={…} declares, and where.
func letNames(src []byte, t jsxTag) ([]string, []int) {
	for _, a := range t.attrs {
		if a.name != "let" || a.valStart == 0 {
			continue
		}
		var names []string
		var offs []int
		for i := a.valStart; i < a.valEnd; {
			if !isNameByte(src[i]) {
				i++
				continue
			}
			j := i
			for j < a.valEnd && isNameByte(src[j]) {
				j++
			}
			names, offs = append(names, string(src[i:j])), append(offs, i)
			i = j
		}
		return names, offs
	}
	return nil, nil
}

// slotDefinition is where the field a slot tag fills is declared.
func (p *proxy) slotDefinition(path string, src []byte, s *shapeScan, off int) (map[string]any, bool) {
	_, _, pr, ok := p.slotAt(path, src, s, off)
	if !ok || pr.file == "" || pr.line < 1 {
		return nil, false
	}
	lines := strings.Split(string(p.sourceText(pr.file)), "\n")
	at := lspPosition{Line: uint32(pr.line - 1)}
	end := at
	if pr.line <= len(lines) {
		if c := identIndex(lines[pr.line-1], pr.name); c >= 0 {
			at.Character, end.Character = uint32(c), uint32(c+len(pr.name))
		}
	}
	return map[string]any{"uri": pathToURI(p.display(pr.file)), "range": lspRange{at, end}}, true
}

// identIndex is where name stands as a whole identifier in line, -1 when
// it doesn't.
func identIndex(line, name string) int {
	for from := 0; name != ""; {
		i := strings.Index(line[from:], name)
		if i < 0 {
			return -1
		}
		i += from
		if (i == 0 || !isNameByte(line[i-1])) && (i+len(name) == len(line) || !isNameByte(line[i+len(name)])) {
			return i
		}
		from = i + 1
	}
	return -1
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
	if pr, ok := info.slotProp(strings.TrimPrefix(l.slot, "slot:")); ok && l.index < len(pr.params) {
		typ := pr.params[l.index]
		if t := p.letType(path, l.off); t != "" {
			typ = t
		}
		return "**slot value** `" + r.name + " " + typ + "` · " + where + ": value " + fmt.Sprint(l.index+1) + " of `" + p.letFunc(info, pr) + "`, what `<" + l.comp + ">` passes"
	}
	return "**slot value** `" + r.name + "` · " + where
}

// letFunc is the function whose values a slot's let names: the field's
// own type, a repeated slot's element's func field.
func (p *proxy) letFunc(info *tagInfo, pr tagProp) string {
	if pr.kind == slotManyKind {
		for _, ep := range p.elemProps(info, pr) {
			if ep.kind == slotFuncKind {
				return ep.name + " " + ep.typ
			}
		}
	}
	return pr.typ
}

// letType is the type the last build gave the let declared at off in
// path's text: a generic component's type arguments substituted, inferred
// or written. "" when there is no build of it.
func (p *proxy) letType(path string, off int) string {
	p.mu.Lock()
	vf := p.virtual[p.bySource[path]]
	p.mu.Unlock()
	if vf == nil {
		return ""
	}
	from := vf.fromOff(off)
	for _, l := range vf.lets {
		if l.Off == from && !vf.goneAt(l.Off) {
			return l.Type
		}
	}
	return ""
}
