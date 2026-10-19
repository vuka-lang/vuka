package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// Each piece of JSX — a tree's root, a block's body, a component tag's
// children — becomes a template: a package-level ui.Template of its markup,
// written before the declaration holding it, every expression, block and
// component tag a numbered hole, filled in document order where the JSX was:
//
//	<li key={u.ID}>{u.Name}</li>   →   var listLi = ui.Template(`<li key="{{0}}">{{1}}</li>`)
//	                                   listLi.Render(u.ID, u.Name)
//
// Blocks are Go: {for _, x := range xs {…}} is ui.Each over a slice, other
// loops append to a []ui.Node, {if} and {match} assign one ui.Node and pass
// it as ui.If(n != nil, n).

// tmpl is a template being written: its markup and its holes' values.
type tmpl struct {
	b    strings.Builder
	args []func()
}

// tmplDecl is a template written: its variable, markup and the source it
// maps to.
type tmplDecl struct {
	name, markup string
	at           int
}

// piece writes ns as a template's Render, or as the call it is when ns is
// one function component's tag.
func (w *jsxWriter) piece(ns []jsxNode) {
	ns = flatten(ns)
	if len(ns) == 1 {
		switch n := ns[0].(type) {
		case *jsxElem:
			// A function component is its call; a keyed stateful one, in a
			// block, ui.Key: a list's components take the list's place.
			if c := n.comp; c != nil && c.resolved && !c.island && (!c.stateful || c.key != nil && w.inBlock) {
				w.comp(c)
				return
			}
		case *jsxBlock:
			if !w.isLoop(n) || w.each(n) {
				if w.isLoop(n) {
					return
				}
				w.block(n)
				return
			}
		}
	}
	tp := &tmpl{}
	w.markup(tp, ns, "", "", false)
	if src := tp.b.String(); strings.Contains(src, "\n") && !strings.HasPrefix(src, "\n") && isMarkup(ns[0]) {
		tp.b.Reset()
		tp.b.WriteString("\n" + src)
	}
	name := w.tmplName(ns)
	at := nodesStart(ns)
	if at == 0 {
		at = w.t.start
	}
	w.decls = append(w.decls, tmplDecl{name, tp.b.String(), at})
	w.gen(name + ".Render(")
	lines := len(tp.args) > 5 || len(tp.args) > 1 && hasBlock(ns)
	for i, a := range tp.args {
		switch {
		case lines:
			w.gen("\n")
		case i > 0:
			w.gen(", ")
		}
		a()
		if lines {
			w.gen(",")
		}
	}
	if lines {
		w.gen("\n")
	}
	w.gen(")")
}

// hasBlock reports whether ns hold a block outside a component's tag.
func hasBlock(ns []jsxNode) bool {
	for _, n := range ns {
		switch n := n.(type) {
		case *jsxBlock:
			return true
		case *jsxElem:
			if n.comp == nil && hasBlock(n.kids) {
				return true
			}
		}
	}
	return false
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

// hole writes a hole for a value written later, in document order.
func (tp *tmpl) hole(write func()) {
	tp.b.WriteString("{{" + strconv.Itoa(len(tp.args)) + "}}")
	tp.args = append(tp.args, write)
}

// markup writes nodes: each on a line of its own, indented, where the
// whitespace between them means nothing (as JSX's doesn't), else inline.
//
// line is the indent of the line they start on, child their lines'.
func (w *jsxWriter) markup(tp *tmpl, ns []jsxNode, line, child string, inline bool) {
	breaks := !inline && layout(ns)
	at := line
	if breaks {
		at = child
	}
	for i, n := range ns {
		if breaks && (i == 0 || isMarkup(ns[i-1]) && isMarkup(n)) {
			tp.b.WriteString("\n" + child)
		}
		w.node(tp, n, at, inline)
	}
	if breaks && child != line {
		tp.b.WriteString("\n" + line)
	}
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
		tp.hole(func() { w.holeValue(n) })
	case *jsxBlock:
		tp.hole(func() { w.block(n) })
	case *jsxElem:
		switch {
		case n.frag:
			w.markup(tp, flatten(n.kids), indent, indent, inline)
		case n.comp != nil:
			c := n.comp
			tp.hole(func() { w.comp(c) })
		default:
			w.element(tp, n, indent, inline)
		}
	}
}

// element writes an element, its attributes and its children.
func (w *jsxWriter) element(tp *tmpl, el *jsxElem, indent string, inline bool) {
	tp.b.WriteString("<" + el.tag)
	for _, a := range el.attrs {
		tp.b.WriteString(" " + a.name)
		switch a.kind {
		case 's':
			tp.b.WriteString(`="` + escapeAttr(a.val) + `"`)
		case 'e':
			tp.b.WriteString(`="`)
			a := a
			tp.hole(func() { w.attrValue(a) })
			tp.b.WriteString(`"`)
		}
	}
	kids := flatten(el.kids)
	if len(kids) == 0 {
		tp.b.WriteString("/>")
		return
	}
	tp.b.WriteString(">")
	switch el.tag {
	case "script", "style":
		for _, k := range kids {
			if t, ok := k.(*jsxText); ok {
				tp.b.WriteString(escapeRaw(t.val))
			} else {
				w.node(tp, k, indent, true)
			}
		}
	default:
		w.markup(tp, kids, indent, indent+"  ", inline || el.tag == "pre" || el.tag == "textarea" || el.tag == "title")
	}
	tp.b.WriteString("</" + el.tag + ">")
}

// escapeText is text as markup: what ui.Template decodes back to it.
func escapeText(s string) string {
	return textEscaper.Replace(s)
}

func escapeAttr(s string) string { return attrEscaper.Replace(s) }

// escapeRaw is a script's or a style's text, kept as it is but for what a
// raw string or a hole would read.
func escapeRaw(s string) string { return rawEscaper.Replace(s) }

var (
	textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", "`", "&#96;", "{{", "&#123;{")
	attrEscaper = strings.NewReplacer("&", "&amp;", `"`, "&quot;", "`", "&#96;", "{{", "&#123;{")
	rawEscaper  = strings.NewReplacer("`", "` + \"`\" + `")
)

// block writes a {for}, {if} or {match} as a value: ui.Each over a slice,
// a []ui.Node built by a loop, or the one node an if or a match picks,
// passed as ui.If.
func (w *jsxWriter) block(n *jsxBlock) {
	q := w.q
	outer := w.inBlock
	w.inBlock = true
	defer func() { w.inBlock = outer }()
	if w.isLoop(n) {
		if w.each(n) {
			return
		}
		if x := w.rangeLen(n); x != nil {
			w.gen("func() []" + q + "Node {\n__nodes := make([]" + q + "Node, 0, len(")
			w.copy(*x)
			w.gen("))\n")
		} else {
			w.gen("func() []" + q + "Node {\nvar __nodes []" + q + "Node\n")
		}
		w.keep(n.heads[0])
		if len(n.bodies[0]) > 0 {
			w.gen("\n__nodes = append(__nodes, ")
			w.piece(n.bodies[0])
			w.gen(")")
		}
		w.gen("\n}\nreturn __nodes\n}()")
		return
	}
	w.gen("func() " + q + "Node {\nvar __node " + q + "Node\n")
	body := func(ns []jsxNode) {
		if len(ns) > 0 {
			w.gen("\n__node = ")
			w.piece(ns)
			w.gen("\n")
		}
	}
	if m := n.m; m != nil {
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
	w.gen("\nreturn " + q + "If(__node != nil, __node)\n}()")
}

// isLoop reports whether a block is a for.
func (w *jsxWriter) isLoop(n *jsxBlock) bool {
	return n.m == nil && len(n.heads) == 1 && strings.HasPrefix(string(w.f.src[n.heads[0].start:n.heads[0].end]), "for")
}

// each writes {for _, x := range xs { piece }} over a slice as
// ui.Each(xs, func(x T) ui.Node { return piece }), once xs's type is known.
func (w *jsxWriter) each(n *jsxBlock) bool {
	e := w.f.eng
	if !w.final || len(n.heads) != 1 || e.info == nil || w.f.ast == nil {
		return false
	}
	outer := w.inBlock
	w.inBlock = true
	defer func() { w.inBlock = outer }()
	rs := e.rangeAt(w.f, n.heads[0].start)
	if rs == nil || rs.Tok != token.DEFINE && rs.Key != nil {
		return false
	}
	if k, ok := rs.Key.(*ast.Ident); rs.Key != nil && (!ok || k.Name != "_") {
		return false
	}
	sl, ok := types.Unalias(e.info.TypeOf(rs.X)).(*types.Slice)
	if !ok {
		if s, isSlice := e.info.TypeOf(rs.X).Underlying().(*types.Slice); isSlice {
			sl, ok = s, true
		}
	}
	if !ok {
		return false
	}
	v, ok := rs.Value.(*ast.Ident)
	if rs.Value != nil && !ok {
		return false
	}
	elem := e.typeTextAuto(w.f, sl.Elem())
	w.gen(w.q + "Each(")
	w.keep(span{w.f.orig(rs.X.Pos()), w.f.orig(rs.X.End())})
	w.gen(", func(")
	if v != nil {
		w.copy(span{w.f.orig(v.Pos()), w.f.orig(v.End())})
	} else {
		w.gen("_")
	}
	w.gen(" " + elem + ") " + w.q + "Node {\nreturn ")
	if len(n.bodies[0]) == 0 {
		w.gen("nil")
	} else {
		w.piece(n.bodies[0])
	}
	w.gen("\n})")
	return true
}

// rangeLen is what a loop ranges over when its len is the number of
// iterations, read again without effects: a slice, array or map named by
// a variable or its fields.
func (w *jsxWriter) rangeLen(n *jsxBlock) *span {
	e := w.f.eng
	if !w.final || e.info == nil || w.f.ast == nil {
		return nil
	}
	rs := e.rangeAt(w.f, n.heads[0].start)
	if rs == nil || !plainOperand(rs.X) {
		return nil
	}
	switch e.info.TypeOf(rs.X).Underlying().(type) {
	case *types.Slice, *types.Array, *types.Map:
		return &span{w.f.orig(rs.X.Pos()), w.f.orig(rs.X.End())}
	}
	return nil
}

// plainOperand reports whether x is a variable or a field of one.
func plainOperand(x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		return plainOperand(x.X)
	case *ast.ParenExpr:
		return plainOperand(x.X)
	}
	return false
}

// rangeAt is the range statement whose for keyword is at src offset off.
func (e *engine) rangeAt(f *fileState, off int) *ast.RangeStmt {
	if f.rangeIdxOf != f.ast {
		f.rangeIdx, f.rangeIdxOf = map[int]*ast.RangeStmt{}, f.ast
		ast.Inspect(f.ast, func(n ast.Node) bool {
			if rs, ok := n.(*ast.RangeStmt); ok && f.lowerable(rs.For) {
				f.rangeIdx[f.orig(rs.For)] = rs
			}
			return true
		})
	}
	return f.rangeIdx[off]
}

// tmplName names a template after its declaration: the one it returns by
// the declaration itself (orderRow), others by their first tag too
// (orderRowTd).
func (w *jsxWriter) tmplName(ns []jsxNode) string {
	base := w.t.base
	if base == "" {
		base = "markup"
	}
	cand := base
	if w.seq > 0 || w.t.baseTaken || !w.t.returned {
		cand = base + firstTag(ns)
	}
	w.seq++
	return "__" + w.f.eng.tmplName(cand, w.final)
}

// firstTag is the first element's tag of ns, as a name's part: Li, Text
// when there is none.
func firstTag(ns []jsxNode) string {
	for _, n := range ns {
		if el, ok := n.(*jsxElem); ok && !el.frag && el.comp == nil {
			return exportName(el.tag)
		}
	}
	return "Text"
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

// tmplName is cand, numbered when another template has it: for good once
// final, else for this round.
func (e *engine) tmplName(cand string, final bool) string {
	if e.tmplFixed == nil {
		e.tmplFixed = map[string]bool{}
	}
	if e.tmplRound == nil {
		e.tmplRound = map[string]bool{}
	}
	name := cand
	for i := 2; e.tmplFixed[name] || !final && e.tmplRound[name]; i++ {
		name = cand + strconv.Itoa(i)
	}
	if final {
		e.tmplFixed[name] = true
	} else {
		e.tmplRound[name] = true
	}
	return name
}

// tmplBase is the name a top-level declaration's templates are named
// after: a function's (row), a method's type and name (ordersRow; a Render
// method's type alone), a variable's. taken reports that it is the
// declaration's own name.
func tmplBase(toks []tok, i int) (base string, taken bool) {
	next := func(j int) int {
		for j++; j < len(toks) && trivia(toks[j]); j++ {
		}
		return j
	}
	j := next(i)
	if j >= len(toks) {
		return "", false
	}
	recv := ""
	switch {
	case toks[j].tok == token.LPAREN:
		recv = declName(toks, i)
		for d := 0; j < len(toks); j++ {
			if toks[j].tok == token.LPAREN || toks[j].tok == token.LBRACK {
				d++
			} else if toks[j].tok == token.RPAREN || toks[j].tok == token.RBRACK {
				if d--; d == 0 {
					break
				}
			}
		}
		j = next(j)
	case toks[j].tok == token.LPAREN:
	}
	if j >= len(toks) || toks[j].tok != token.IDENT {
		return "", false
	}
	name := toks[j].lit
	if k := next(j); recv == "" && k+1 < len(toks) && toks[k].tok == token.PERIOD && toks[k+1].tok == token.IDENT {
		recv, name = name, toks[k+1].lit // a static method: func User.New
	}
	if recv == "" {
		return lowerFirst(name), lowerFirst(name) == name
	}
	if name == "Render" {
		return lowerFirst(recv), lowerFirst(recv) == recv
	}
	return lowerFirst(recv) + exportName(name), false
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
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
