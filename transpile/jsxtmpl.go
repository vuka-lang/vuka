package transpile

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/scanner"
	"go/token"
	"go/types"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Each piece of JSX — a tree's root, a block's body, a component tag's
// children — becomes a ui.HTML call: its markup with every expression a
// hole labelled with the expression's Go, the values after it in the order
// the labels first appear:
//
//	<li key={u.ID}>{u.Name}</li>   →   ui.HTML(`<li key={u.ID}>{u.Name}</li>`, u.ID, u.Name)
//
// Blocks are Go statements before the call, each appending what it picks
// to a []ui.Node a hole names.
// So is a value whose Go can't be a label (a component holding markup):
//
//	var lis []ui.Node
//	for _, u := range users {
//		lis = append(lis, ui.HTML(`<li key={u.ID}>{u.Name}</li>`, u.ID, u.Name))
//	}
//	return ui.HTML(`<ul>{lis}</ul>`, lis)
//
// The statements go before the return whose value the tree is, or else into
// a function literal called in its place.

// tmpl is a template being written: its markup and its values.
type tmpl struct {
	b     strings.Builder
	args  []tmplArg
	marks []mark // where labels are in b
}

type mark struct {
	at    int
	label string
}

// label writes a hole's label, remembering where.
func (tp *tmpl) label(l string) {
	tp.marks = append(tp.marks, mark{tp.b.Len(), l})
	tp.b.WriteString(l)
}

// relabel renames the holes labelled from.
func (tp *tmpl) relabel(from, to string) {
	src := tp.b.String()
	var b strings.Builder
	last, d := 0, 0
	for i, m := range tp.marks {
		at := m.at
		tp.marks[i].at += d
		if m.label != from {
			continue
		}
		b.WriteString(src[last:at])
		b.WriteString(to)
		last = at + len(from)
		tp.marks[i].label = to
		tp.marks[i].at = b.Len() - len(to)
		d += len(to) - len(from)
	}
	b.WriteString(src[last:])
	tp.b.Reset()
	tp.b.WriteString(b.String())
}

type tmplArg struct {
	label string
	write func()
}

// stream is generated pieces: an expression, or statements.
type stream struct{ p []jsxPiece }

// expr is what f writes, apart.
func (w *jsxWriter) expr(f func()) []jsxPiece {
	saved := w.out
	w.out = &stream{}
	f()
	out := w.out.p
	w.out = saved
	return out
}

// stmts writes f's text as statements before the tree's expression.
func (w *jsxWriter) stmts(f func()) {
	saved := w.out
	w.out = w.sink
	f()
	w.out = saved
}

func (w *jsxWriter) put(ps []jsxPiece) { w.out.p = append(w.out.p, ps...) }

// piece writes ns as a ui.HTML call, or as the call it is when ns is one
// function component's tag or one Node.
func (w *jsxWriter) piece(ns []jsxNode) {
	ns = flatten(ns)
	if len(ns) == 1 {
		switch n := ns[0].(type) {
		case *jsxBlock:
			// A branch every way of which picks a Node is that Node.
			if w.picksOne(n) {
				w.gen(w.block(n))
				return
			}
		case *jsxElem:
			if len(n.parts) > 0 && n.partFB == nil {
				w.part(n, 0)
				return
			}
			if n.isSuspense(w.f) {
				w.suspense(n)
				return
			}
			// A function component is its call; a keyed stateful one, in a
			// block, ui.Key: a list's components take the list's place.
			if c := n.comp; n.partFB == nil && c != nil && c.resolved && !c.island && (!c.stateful || c.key != nil && w.inBlock) {
				w.comp(c)
				return
			}
		case *jsxHole:
			// A Node is itself.
			if w.isNode(n) {
				w.holeValue(n)
				return
			}
		}
	}
	tp := &tmpl{}
	w.markup(tp, ns, "", "", false)
	src := tp.b.String()
	if strings.Contains(src, "\n") && !strings.HasPrefix(src, "\n") && isMarkup(ns[0]) {
		src = "\n" + src
	}
	w.gen(w.q + "HTML(" + rawString(src))
	lines := len(tp.args) > 5
	for _, a := range tp.args {
		if lines {
			w.gen(",\n")
		} else {
			w.gen(", ")
		}
		a.write()
	}
	if lines {
		w.gen(",\n")
	}
	w.gen(")")
}

// isNode reports whether a hole's value is a Node, once types are known.
func (w *jsxWriter) isNode(n *jsxHole) bool {
	e := w.f.eng
	if !w.final || n.bind != nil || e.info == nil {
		return false
	}
	node := e.nodeType(w.f.target)
	t := e.typeAt(w.f, w.f.cur.fromOrig(n.expr.start), w.f.cur.fromOrigEnd(n.expr.end))
	return node != nil && t != nil && types.AssignableTo(t, node) && !types.IsInterface(t) || t != nil && types.Identical(t, node)
}

// flatten is ns with fragments' children in their place.
func flatten(ns []jsxNode) []jsxNode {
	for _, n := range ns {
		if el, ok := n.(*jsxElem); ok && el.frag {
			var out []jsxNode
			for _, n := range ns {
				if el, ok := n.(*jsxElem); ok && el.frag {
					out = append(out, flatten(el.kids)...)
				} else {
					out = append(out, n)
				}
			}
			return out
		}
	}
	return ns
}

// hole adds the value labelled label, written later; the same label is one
// value.
func (tp *tmpl) hole(label string, write func()) {
	for _, a := range tp.args {
		if a.label == label {
			return
		}
	}
	tp.args = append(tp.args, tmplArg{label, write})
}

// value is a hole's label for what write writes: its Go, or, when that
// can't be a label, a variable declared before the tree named after base.
func (w *jsxWriter) value(tp *tmpl, base string, write func()) string {
	ps := w.expr(write)
	if text, ok := w.labelText(ps); ok {
		label := canonExpr(text)
		seen := false
		for _, a := range tp.args {
			seen = seen || a.label == label
		}
		switch {
		case !seen:
			tp.hole(label, func() { w.put(ps) })
			return label
		case w.final && !strings.Contains(label, "("):
			// The value is the first's: what this one keeps is dropped.
			w.drop(ps)
			return label
		case !w.final:
			// Checked as a value of its own until the last round.
			label += "#" + strconv.Itoa(len(tp.args))
			tp.hole(label, func() { w.put(ps) })
			return label
		}
	}
	name := w.varName(base)
	w.stmts(func() {
		w.gen(name + " := ")
		w.put(ps)
		w.gen("\n")
	})
	tp.hole(name, func() { w.gen(name) })
	return name
}

// holeIn writes {label} for what write writes.
func (w *jsxWriter) holeIn(tp *tmpl, base string, write func()) {
	l := w.value(tp, base, write)
	tp.b.WriteString("{")
	tp.label(l)
	tp.b.WriteString("}")
}

// capture makes the values of tp's first n holes that stmts may change
// variables declared at the sink's piece at: a statement after a hole
// doesn't change what it shows.
func (w *jsxWriter) capture(tp *tmpl, n, at int, stmts []string) {
	var decls []jsxPiece
	for i := range tp.args[:n] {
		a := &tp.args[i]
		if constLabel(a.label) || !changes(stmts, a.label) {
			continue
		}
		name := w.varName(labelBase(a.label))
		ps := w.expr(a.write)
		decls = append(decls, jsxPiece{text: name + " := ", keep: span{-1, -1}})
		decls = append(decls, ps...)
		decls = append(decls, jsxPiece{text: "\n", keep: span{-1, -1}})
		tp.relabel(a.label, name)
		a.label, a.write = name, func() { w.gen(name) }
	}
	w.sink.p = slices.Insert(w.sink.p, at, decls...)
}

// changes reports whether stmts may change what label reads: they call
// something, or name what it names.
func changes(stmts []string, label string) bool {
	names := goIdents(label)
	for _, s := range stmts {
		if strings.Contains(s, "(") {
			return true
		}
		for n := range goIdents(s) {
			if names[n] {
				return true
			}
		}
	}
	return false
}

func goIdents(src string) map[string]bool {
	out := map[string]bool{}
	var sc scanner.Scanner
	fs := token.NewFileSet()
	sc.Init(fs.AddFile("", -1, len(src)), []byte(src), nil, 0)
	for {
		_, tok, lit := sc.Scan()
		if tok == token.EOF {
			return out
		}
		if tok == token.IDENT {
			out[lit] = true
		}
	}
}

// constLabel reports whether a hole's value can't change: a variable of
// the tree's, or a literal.
func constLabel(l string) bool {
	if strings.HasPrefix(l, "__") || l == "true" || l == "false" || l == "nil" {
		return true
	}
	x, err := parser.ParseExpr(l)
	_, lit := x.(*ast.BasicLit)
	return err == nil && lit
}

// labelBase is what a captured value is named after: its expression's last
// name.
func labelBase(l string) string {
	x, err := parser.ParseExpr(l)
	if err != nil {
		return "value"
	}
	switch x := x.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return lowerFirst(x.Sel.Name)
	}
	return "value"
}

// drop writes ps's kept spans as dropped: no text, their rewrites gone.
func (w *jsxWriter) drop(ps []jsxPiece) {
	for _, p := range ps {
		if p.keep.start >= 0 {
			w.out.p = append(w.out.p, jsxPiece{keep: p.keep, copy: true, drop: true})
		}
	}
}

// labelText is the Go ps write when it can be a label: one line, no raw
// string, no JSX inside, no name Vuka declares.
func (w *jsxWriter) labelText(ps []jsxPiece) (string, bool) {
	var b strings.Builder
	for _, p := range ps {
		if p.keep.start < 0 {
			b.WriteString(p.text)
			continue
		}
		for _, t := range w.f.jsx {
			if t != w.t && t.start >= p.keep.start && t.start < p.keep.end {
				return "", false
			}
		}
		var inner edits
		for _, e := range w.f.fixed {
			if e.own == nil && p.keep.start <= e.start && e.end <= p.keep.end && e.start < p.keep.end {
				inner = append(inner, e)
			}
		}
		text, _ := inner.sorted().applyRange(w.f.src, p.keep.start, p.keep.end, nil)
		b.Write(text)
	}
	s := b.String()
	return s, s != "" && !strings.ContainsAny(s, "\n`") && !strings.Contains(s, "__")
}

// canonExpr is a label's text as gofmt writes the expression as an
// argument.
func canonExpr(s string) string {
	x, err := parser.ParseExpr("_(" + s + ")")
	if err != nil {
		return strings.Join(strings.Fields(s), " ")
	}
	var b strings.Builder
	if printer.Fprint(&b, token.NewFileSet(), x) != nil {
		return strings.Join(strings.Fields(s), " ")
	}
	out := strings.TrimSuffix(strings.TrimPrefix(b.String(), "_("), ")")
	if strings.Contains(out, "\n") {
		return strings.Join(strings.Fields(s), " ")
	}
	return out
}

// varName is a fresh variable for a value declared before the tree.
func (w *jsxWriter) varName(base string) string {
	if base == "" {
		base = "node"
	}
	if w.names == nil {
		w.names = map[string]bool{}
	}
	name := base
	for i := 2; w.names[name]; i++ {
		name = base + strconv.Itoa(i)
	}
	w.names[name] = true
	return "__" + name
}

// markup writes nodes: each on a line of its own, indented, where the
// whitespace between them means nothing (as JSX's doesn't), else inline.
//
// line is the indent of the line they start on, child their lines'.
func (w *jsxWriter) markup(tp *tmpl, ns []jsxNode, line, child string, inline bool) {
	ns = w.withoutFallbacks(withoutAssets(ns))
	breaks := !inline && layout(ns)
	at := line
	if breaks {
		at = child
	}
	for i, n := range ns {
		sink, ran, args := len(w.sink.p), len(w.ran), len(tp.args)
		if b, ok := n.(*jsxBlock); ok && b.stmt {
			w.block(b)
		} else {
			if breaks && (i == 0 || isMarkup(ns[i-1]) && isMarkup(n)) {
				tp.b.WriteString("\n" + child)
			}
			w.node(tp, n, at, inline)
		}
		if len(w.ran) > ran {
			w.capture(tp, args, sink, w.ran[ran:])
		}
	}
	if breaks && child != line {
		tp.b.WriteString("\n" + line)
	}
}

// withoutAssets is ns without the component scripts and scoped stylesheets
// the vuka command bundles.
func withoutAssets(ns []jsxNode) []jsxNode {
	for i, n := range ns {
		if el, ok := n.(*jsxElem); ok && el.asset {
			out := append([]jsxNode{}, ns[:i]...)
			for _, m := range ns[i+1:] {
				if el, ok := m.(*jsxElem); !ok || !el.asset {
					out = append(out, m)
				}
			}
			return trimBody(out)
		}
	}
	return ns
}

// isMarkup reports whether n isn't text: a line may break next to it.
func isMarkup(n jsxNode) bool {
	_, text := n.(*jsxText)
	return !text
}

// layout reports whether ns go on lines of their own: two nodes that
// aren't text meet somewhere, and no text at either end has a space a line
// break would take.
func layout(ns []jsxNode) bool {
	if len(ns) == 0 {
		return false
	}
	if t, ok := ns[0].(*jsxText); ok && strings.HasPrefix(t.val, " ") {
		return false
	}
	if t, ok := ns[len(ns)-1].(*jsxText); ok && strings.HasSuffix(t.val, " ") {
		return false
	}
	for i := 1; i < len(ns); i++ {
		if isMarkup(ns[i-1]) && isMarkup(ns[i]) {
			return true
		}
	}
	return false
}

func (w *jsxWriter) node(tp *tmpl, n jsxNode, indent string, inline bool) {
	switch n := n.(type) {
	case *jsxText:
		tp.b.WriteString(escapeText(n.val))
	case *jsxHole:
		w.holeIn(tp, "value", func() { w.holeValue(n) })
	case *jsxBlock:
		if name := w.block(n); name != "" {
			tp.b.WriteString("{" + name + "}")
			tp.hole(name, func() { w.gen(name) })
		}
	case *jsxElem:
		switch {
		case n.frag:
			w.markup(tp, flatten(n.kids), indent, indent, inline)
		case len(n.parts) > 0:
			w.holeIn(tp, "part", func() { w.part(n, 0) })
		case n.isSuspense(w.f):
			w.holeIn(tp, "suspense", func() { w.suspense(n) })
		case n.fallback:
			w.holeIn(tp, "slot", func() { w.fallbackValue(n) })
		case n.comp != nil:
			c := n.comp
			w.holeIn(tp, lowerFirst(exportName(lastName(n.tag))), func() { w.comp(c) })
		default:
			w.element(tp, n, indent, inline)
		}
	}
}

var renderRecv = regexp.MustCompile(`func\s*\(\s*(\w+)\s+\*?\w+\s*\)\s*Render\s*\(`)

// rootProps is ui.Props(recv) for the root element of a Render method's
// JSX whose children hold a component script — the props the script reads
// — and ui.States(recv) when it calls ctx.state.
func (w *jsxWriter) rootProps(el *jsxElem) (props, states string) {
	if w.f.html != nil || el != w.t.root {
		return "", ""
	}
	var script *jsxElem
	for _, k := range el.kids {
		if a, ok := k.(*jsxElem); ok && a.asset && a.tag == "script" {
			script = a
		}
	}
	m := renderRecv.FindSubmatch(w.f.src[w.t.declOff:w.t.start])
	if script == nil || m == nil {
		return "", ""
	}
	props = w.q + "Props(" + string(m[1]) + ")"
	if strings.Contains(string(w.f.src[script.start:script.end]), ".state(") {
		states = w.q + "States(" + string(m[1]) + ")"
	}
	return props, states
}

// lastName is a tag's last name: ui.Card is Card.
func lastName(tag string) string {
	if i := strings.LastIndexByte(tag, '.'); i >= 0 {
		return tag[i+1:]
	}
	return tag
}

// htmlName is an attribute's HTML name: JSX's className is class, htmlFor
// for.
func htmlName(name string) string {
	switch name {
	case "className":
		return "class"
	case "htmlFor":
		return "for"
	}
	return name
}

// element writes an element, its attributes and its children.
func (w *jsxWriter) element(tp *tmpl, el *jsxElem, indent string, inline bool) {
	kids := flatten(el.kids)
	if (el.tag == "script" || el.tag == "style") && !allText(kids) {
		// Braces in a script or style are its text: one built with values is
		// a value.
		w.holeIn(tp, el.tag, func() { w.rawElement(el, kids) })
		return
	}
	tp.b.WriteString("<" + el.tag)
	if props, states := w.rootProps(el); props != "" {
		tp.b.WriteString(" data-vk-p=")
		w.holeIn(tp, "props", func() { w.gen(props) })
		if states != "" {
			tp.b.WriteString(" data-vk-st=")
			w.holeIn(tp, "states", func() { w.gen(states) })
		}
	}
	style, props := el.styleAttrs()
	for _, a := range el.attrs {
		a := a
		if a.name == "is:inline" {
			continue
		}
		if len(props) > 0 && (a == style || a == props[0] && style == nil) {
			// style:--x={v}: the style attribute with the properties in it.
			tp.b.WriteString(" style=")
			w.holeIn(tp, "style", func() { w.cssVars(style, props) })
			continue
		}
		if len(props) > 0 && strings.HasPrefix(a.name, "style:") {
			continue
		}
		if a.name == "bind:value" {
			tp.b.WriteString(" {")
			tp.label(w.value(tp, "bind", func() { w.bindValue(a) }))
			tp.b.WriteString("...}")
			continue
		}
		if a.model {
			tp.b.WriteString(" {")
			tp.label(w.value(tp, "model", func() {
				w.gen(w.q + "Model(")
				w.modelSpec(el, a)
				w.gen(", ")
				w.refArgs(a)
				w.gen(")")
			}))
			tp.b.WriteString("...}")
			continue
		}
		if a.ref && el.comp == nil && w.addressable(a) {
			tp.b.WriteString(" for=")
			w.holeIn(tp, "value", func() { w.modelCall("ModelID", "", a) })
			continue
		}
		if a.kind == 'r' {
			l := w.value(tp, "attrs", func() { w.keep(a.expr) })
			tp.b.WriteString(" {")
			tp.label(l)
			tp.b.WriteString("...}")
			continue
		}
		tp.b.WriteString(" " + htmlName(a.name))
		switch a.kind {
		case 's':
			tp.b.WriteString(`="` + escapeAttr(a.val) + `"`)
		case 'e':
			tp.b.WriteString("=")
			w.holeIn(tp, "value", func() { w.attrValue(a) })
		case 'p':
			tp.b.WriteString(`="`)
			for _, pt := range a.parts {
				if pt.expr.end == 0 {
					tp.b.WriteString(escapeAttr(pt.text))
					continue
				}
				pt := pt
				w.holeIn(tp, "value", func() { w.keep(pt.expr) })
			}
			tp.b.WriteString(`"`)
		}
	}
	if sel := w.sel; el.tag == "option" && sel != nil && attrNamed(el, "selected") == nil {
		if v := attrNamed(el, "value"); v != nil && v.kind != 'p' {
			tp.b.WriteString(" selected=")
			w.holeIn(tp, "value", func() {
				w.gen(w.q + "ModelSelected(")
				w.attrValue(v)
				w.gen(", ")
				w.refArgs(sel)
				w.gen(")")
			})
		}
	}
	if m := modelOf(el); m != nil && !hasElems(kids) && (el.tag == "textarea" || el.tag == "select") {
		tp.b.WriteString(">")
		if el.tag == "textarea" {
			w.holeIn(tp, "text", func() { w.modelCall("ModelText", "", m) })
		} else {
			w.holeIn(tp, "options", func() { w.modelCall("ModelOptions", "", m) })
		}
		tp.b.WriteString("</" + el.tag + ">")
		return
	}
	if len(kids) == 0 {
		tp.b.WriteString("/>")
		return
	}
	tp.b.WriteString(">")
	if el.tag == "select" {
		saved := w.sel
		w.sel = modelOf(el)
		defer func() { w.sel = saved }()
	}
	switch el.tag {
	case "script", "style":
		for _, k := range kids {
			tp.b.WriteString(escapeRaw(k.(*jsxText).val))
		}
	default:
		w.markup(tp, kids, indent, indent+"  ", inline || el.tag == "pre" || el.tag == "textarea" || el.tag == "title")
	}
	tp.b.WriteString("</" + el.tag + ">")
}

// cssVars writes a style attribute with style:prop attributes as
// ui.CSSVars(style, "prop", value, …).
func (w *jsxWriter) cssVars(style *jsxAttr, props []*jsxAttr) {
	w.gen(w.q + "CSSVars(")
	if style == nil {
		w.gen("nil")
	} else {
		w.attrValue(style)
	}
	for _, a := range props {
		w.gen(", " + strconv.Quote(strings.TrimPrefix(a.name, "style:")) + ", ")
		w.attrValue(a)
	}
	w.gen(")")
}

// bindValue writes bind:value={p.Q} as ui.BindValue(p, &p.Q); a bare name
// of a file's template is its receiver's field.
func (w *jsxWriter) bindValue(a *jsxAttr) {
	w.gen(w.q + "BindValue(")
	text := string(w.f.src[a.expr.start:a.expr.end])
	if dot := strings.LastIndexByte(text, '.'); dot >= 0 {
		w.keep(span{a.expr.start, a.expr.start + dot})
	} else {
		w.gen(w.f.html.recv)
	}
	w.gen(", &")
	w.keep(a.expr)
	w.gen(")")
}

func allText(ns []jsxNode) bool {
	for _, n := range ns {
		if _, ok := n.(*jsxText); !ok {
			return false
		}
	}
	return true
}

// rawElement writes a script or style with values as ui.El.
func (w *jsxWriter) rawElement(el *jsxElem, kids []jsxNode) {
	q := w.q
	w.gen(q + "El(" + strconv.Quote(el.tag) + ", ")
	attrs := 0
	for _, a := range el.attrs {
		if a.name != "is:inline" {
			attrs++
		}
	}
	if attrs == 0 {
		w.gen("nil")
	} else {
		w.gen("[]" + q + "Attr{")
		n := 0
		for _, a := range el.attrs {
			if a.name == "is:inline" {
				continue
			}
			if n++; n > 1 {
				w.gen(", ")
			}
			w.gen("{Name: " + strconv.Quote(htmlName(a.name)) + ", Value: ")
			w.attrValue(a)
			w.gen("}")
		}
		w.gen("}")
	}
	for _, k := range kids {
		w.gen(", ")
		switch k := k.(type) {
		case *jsxText:
			w.gen(q + "Safe(" + strconv.Quote(k.val) + ")")
		case *jsxHole:
			if k.json {
				w.gen(q + "JSON(")
			} else {
				w.gen(q + "Child(")
			}
			w.holeValue(k)
			w.gen(")")
		default:
			w.piece([]jsxNode{k})
		}
	}
	w.gen(")")
}

// escapeText is text as markup: what ui.HTML decodes back to it.
func escapeText(s string) string {
	return textEscaper.Replace(s)
}

func escapeAttr(s string) string { return attrEscaper.Replace(s) }

// escapeRaw is a script's or a style's text, kept as it is but for what a
// raw string would read.
func escapeRaw(s string) string { return rawEscaper.Replace(s) }

var (
	textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", "`", "&#96;", "{", "{{")
	attrEscaper = strings.NewReplacer("&", "&amp;", `"`, "&quot;", "`", "&#96;", "{", "{{")
	rawEscaper  = strings.NewReplacer("`", "` + \"`\" + `")
)

// block writes a {for}, {if} or {match} as statements before the tree —
// a loop building a []ui.Node, the nodes it makes its items, a branch
// setting a ui.Node — and returns the variable.
func (w *jsxWriter) block(n *jsxBlock) string {
	q := w.q
	outer := w.inBlock
	w.inBlock = true
	defer func() { w.inBlock = outer }()
	if n.stmt {
		w.ran = append(w.ran, string(w.f.src[n.heads[0].start:n.heads[0].end]))
		w.stmts(func() {
			w.keep(n.heads[0])
			w.gen("\n")
		})
		return ""
	}
	if w.isLoop(n) {
		name := w.varName(lowerFirst(w.nodesName(firstBody(n))) + "s")
		w.stmts(func() {
			w.gen("var " + name + " []" + q + "Node\n")
			w.keep(n.heads[0])
			if len(n.bodies[0]) > 0 {
				e := w.expr(func() { w.piece(n.bodies[0]) })
				w.gen("\n" + name + " = append(" + name + ", ")
				w.put(e)
				w.gen(")")
			}
			w.gen("\n}\n")
		})
		return name
	}
	// A branch picks one Node: the block is that Node, nil when none.
	name := w.varName(lowerFirst(w.nodesName(firstBody(n))))
	w.stmts(func() {
		w.gen("var " + name + " " + q + "Node\n")
		body := func(ns []jsxNode) {
			if len(ns) > 0 {
				e := w.expr(func() { w.piece(ns) })
				w.gen("\n" + name + " = ")
				w.put(e)
				w.gen("\n")
			}
		}
		if n.sw {
			w.keep(n.heads[0])
			for i, h := range n.heads[1:] {
				w.gen("\n")
				w.keep(h)
				body(n.cases[i])
			}
			w.gen("\n")
			w.keep(span{n.rbrace, n.rbrace + 1})
		} else if m := n.m; m != nil {
			w.keep(span{m.start, m.lbrace + 1})
			for i, c := range m.cases {
				if i == 0 || len(n.cases[i-1]) == 0 {
					w.gen("\n")
				}
				w.keep(span{c.start, c.colon + 1})
				body(n.cases[i])
			}
			if len(n.cases[len(n.cases)-1]) == 0 {
				w.gen("\n")
			}
			w.keep(span{m.rbrace, m.rbrace + 1})
		} else {
			for i, h := range n.heads {
				if i > 0 {
					w.gen("} ")
				}
				w.keep(h)
				body(n.bodies[i])
			}
			w.gen("}")
		}
		w.gen("\n")
	})
	return name
}

// nodesName is what a block's variable is named after: its first element's
// tag, component, or function called.
func (w *jsxWriter) nodesName(ns []jsxNode) string {
	for _, n := range flatten(ns) {
		switch n := n.(type) {
		case *jsxElem:
			if n.comp != nil {
				return exportName(lastName(n.tag))
			}
			return exportName(n.tag)
		case *jsxHole:
			x, err := parser.ParseExpr(string(w.f.src[n.expr.start:n.expr.end]))
			if c, ok := x.(*ast.CallExpr); ok && err == nil {
				switch f := c.Fun.(type) {
				case *ast.Ident:
					return exportName(f.Name)
				case *ast.SelectorExpr:
					return exportName(f.Sel.Name)
				}
			}
		}
	}
	return "Text"
}

// firstBody is a block's first body with nodes.
func firstBody(n *jsxBlock) []jsxNode {
	for _, b := range append(n.bodies, n.cases...) {
		if len(b) > 0 {
			return b
		}
	}
	return nil
}

// picksOne reports whether a block is a branch that picks a Node whichever
// way it goes: an if with an else, a switch with a default, a match, each
// way with markup.
func (w *jsxWriter) picksOne(n *jsxBlock) bool {
	if n.stmt || w.isLoop(n) {
		return false
	}
	ways := n.bodies
	if n.sw || n.m != nil {
		ways = n.cases
	}
	for _, b := range ways {
		if len(b) == 0 {
			return false
		}
	}
	switch {
	case n.m != nil:
		return true
	case n.sw:
		for _, h := range n.heads[1:] {
			if strings.HasPrefix(strings.TrimSpace(string(w.f.src[h.start:h.end])), "default") {
				return true
			}
		}
		return false
	}
	last := strings.Fields(string(w.f.src[n.heads[len(n.heads)-1].start:n.heads[len(n.heads)-1].end]))
	return len(n.heads) > 1 && len(last) > 0 && last[0] == "else" && (len(last) == 1 || last[1] != "if")
}

// isLoop reports whether a block is a for.
func (w *jsxWriter) isLoop(n *jsxBlock) bool {
	return n.m == nil && len(n.heads) == 1 && strings.HasPrefix(string(w.f.src[n.heads[0].start:n.heads[0].end]), "for")
}

// exportName is a tag as an exported Go name: my-chart is MyChart.
func exportName(tag string) string {
	var b strings.Builder
	up := true
	for _, r := range tag {
		switch {
		case r == '-' || r == ':' || r == '.' || r == '_':
			up = true
		case up:
			b.WriteString(strings.ToUpper(string(r)))
			up = false
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// renameDoc follows a declaration at off renamed to name in its doc
// comment: a first word that is one of olds (its names in the source)
// becomes name.
func (f *fileState) renameDoc(off int, name string, olds ...string) {
	for start := f.leadStart(off); start < off; {
		end := start + bytes.IndexByte(f.src[start:off], '\n') + 1
		if end <= start {
			end = off
		}
		inAttr := false
		for _, a := range f.attrs {
			inAttr = inAttr || a.start <= start && start < a.end || start <= a.start && a.start < end
		}
		l := f.src[start:end]
		w := start + len(l) - len(bytes.TrimLeft(l, " \t"))
		if !inAttr && bytes.HasPrefix(f.src[w:end], []byte("// ")) {
			w += 3
			for _, old := range olds {
				if rest := bytes.TrimRight(f.src[w:end], "\r\n"); bytes.HasPrefix(rest, []byte(old)) && !continuesIdent(rest[len(old):]) {
					// to the line's end, so no //line marker follows it
					f.add(w, w+len(rest), name+string(rest[len(old):]))
					return
				}
			}
			return
		}
		start = end
	}
}

// continuesIdent reports whether rest, what follows a name, continues it:
// more of the identifier, or a selector (Post in Post.Live).
func continuesIdent(rest []byte) bool {
	ident := func(b byte) bool {
		return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= 0x80
	}
	return len(rest) > 0 && (ident(rest[0]) || rest[0] == '.' && len(rest) > 1 && ident(rest[1]))
}

// leadStart is where the top-level declaration at off starts with what
// leads it: its doc comment and its decorators.
func (f *fileState) leadStart(off int) int {
	start := lineStart(f.src, off)
	for start > 0 {
		prev := lineStart(f.src, start-1)
		for _, a := range f.attrs {
			if a.start < start && start-1 < a.end {
				prev = lineStart(f.src, a.start)
			}
		}
		l := strings.TrimSpace(string(f.src[prev:start]))
		if l == "" || !(strings.HasPrefix(l, "//") || strings.HasPrefix(l, "/*") || strings.HasPrefix(l, "*") || strings.HasPrefix(l, "@")) {
			break
		}
		start = prev
	}
	return start
}
