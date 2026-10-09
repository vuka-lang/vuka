package transpile

import (
	"go/ast"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

// TemplxPath is the package with the bridge to templ components.
const TemplxPath = RuntimePath + "/templx"

const templPath = "github.com/a-h/templ"

// jsxComp is a component tag, <UserCard user={u} admin />: a call of the Go
// function it names, decided once that function's type is known. Until then
// the tag lowers to a placeholder that type-checks its expressions in place.
type jsxComp struct {
	el         *jsxElem
	resolved   bool
	dead       bool
	fail       string
	props      string     // the props struct literal's type; "" when attributes are parameters
	args       []*compArg // in the call's order
	try        bool       // the function returns (Node, error)
	withKids   bool       // children go through templx.WithChildren
	closure    []*compArg // when bound out of order: the arguments taken by a function literal, in source order
	closureTyp []string
}

// compArg is one argument of a component call.
type compArg struct {
	key  string   // "Field: " for a props struct
	attr *jsxAttr // the value of an attribute
	kids bool     // the children, as a Fragment
	text string   // a generated value: a zero value
	name string   // the function literal's parameter, in the closure form
	typ  types.Type
}

func (w *jsxWriter) comp(c *jsxComp) {
	el, rt := c.el, w.rt
	tag := span{el.start + 1, el.tagEnd}
	if !c.resolved {
		w.gen(rt + ".Fragment(" + rt + ".Child(")
		w.keep(tag)
		w.gen("), ")
		for _, a := range el.attrs {
			if a.kind == 'e' {
				w.gen(rt + ".Child(")
				w.keep(a.expr)
				w.gen("), ")
			}
		}
		w.kids(el.kids, false)
		w.gen(")")
		return
	}
	value := func(a *compArg, named bool) {
		switch {
		case named && a.name != "":
			w.gen(a.name)
		case a.attr != nil:
			w.attrValue(a.attr)
		case a.kids:
			w.gen(rt + ".Fragment(")
			w.kids(el.kids, false)
			w.gen(")")
		default:
			w.gen(a.text)
		}
	}
	var kidsArg *compArg
	if c.withKids {
		kidsArg = &compArg{kids: true}
		if c.closure != nil {
			kidsArg = c.closure[len(c.closure)-1]
		}
	}
	if c.closure != nil {
		var params []string
		for i, a := range c.closure {
			params = append(params, a.name+" "+c.closureTyp[i])
		}
		w.gen("func(" + strings.Join(params, ", ") + ") " + rt + ".Node { return ")
	}
	if c.withKids {
		w.gen(w.f.importAs(TemplxPath, "__templx") + ".WithChildren(")
	}
	if c.try {
		w.gen(rt + ".Try(")
	}
	w.keep(tag)
	w.gen("(")
	if c.props != "" {
		w.gen(c.props + "{")
	}
	for _, a := range c.args {
		w.gen(a.key)
		value(a, true)
		w.gen(", ")
	}
	if c.props != "" {
		w.gen("}")
	}
	w.gen(")")
	if c.try {
		w.gen(")")
	}
	if c.withKids {
		w.gen(", ")
		value(kidsArg, true)
		w.gen(")")
	}
	if c.closure != nil {
		w.gen(" }(")
		for _, a := range c.closure {
			value(a, false)
			w.gen(", ")
		}
		w.gen(")")
	}
}

// lowerJSX resolves the components whose functions' types are known; a tree
// whose components are all resolved takes its final edits.
func (e *engine) lowerJSX(f *fileState) {
	for _, t := range f.jsx {
		if t.done {
			continue
		}
		all := true
		for _, c := range t.comps {
			if !c.resolved && !c.dead {
				e.resolveComp(f, c)
			}
			all = all && c.resolved
		}
		if all {
			f.fixed = append(f.fixed, f.jsxEdits(t)...)
			t.done = true
			e.progress = true
		}
	}
}

// nodeType is the runtime's Node.
func (e *engine) nodeType() types.Type {
	for _, p := range e.pkg.Imports() {
		if p.Path() == RuntimePath {
			if obj := p.Scope().Lookup("Node"); obj != nil {
				return obj.Type()
			}
		}
	}
	return nil
}

func isTemplComponent(t, node types.Type) bool {
	if types.Identical(t, node) {
		return true
	}
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == templPath && n.Obj().Name() == "Component"
}

// resolveComp decides the call a component tag makes:
//
//	<UserCard user={u} admin>hi</UserCard>   →   UserCard(u, true, vuka.Fragment(vuka.Text("hi"), ))
//	<ui.Button variant={v} />                →   ui.Button(ui.ButtonProps{Variant: v, })
//
// Attributes bind to parameters by name (or, for a function taking one props
// struct, to its fields); missing ones get zero values, children go to a
// children parameter, else to a templ component through templx.WithChildren.
func (e *engine) resolveComp(f *fileState, c *jsxComp) {
	el := c.el
	at, end := f.cur.fromOrig(el.start+1), f.cur.fromOrig(el.tagEnd)
	var expr ast.Expr
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if x, ok := n.(ast.Expr); ok && expr == nil && f.off(x.Pos()) == at && f.off(x.End()) == end {
			switch x.(type) {
			case *ast.Ident, *ast.SelectorExpr:
				expr = x
			}
		}
		return expr == nil
	})
	if expr == nil {
		c.fail = "internal: placeholder not found"
		return
	}
	fail := func(off int, format string, args ...any) {
		c.dead = true
		e.errs.add(f.at(off), format, args...)
	}
	var obj types.Object
	switch x := expr.(type) {
	case *ast.Ident:
		obj = e.info.Uses[x]
	case *ast.SelectorExpr:
		obj = e.info.Uses[x.Sel]
	}
	if _, ok := obj.(*types.TypeName); ok {
		fail(el.start, "<%s> is a type: stateful components come in stage 2", el.tag)
		return
	}
	if fn, ok := obj.(*types.Func); ok && fn.Type().(*types.Signature).TypeParams().Len() > 0 {
		fail(el.start, "<%s> is generic; generic components aren't supported yet", el.tag)
		return
	}
	tv, ok := e.info.Types[expr]
	if !ok || isInvalid(tv.Type) {
		c.fail = "its type is unknown"
		return
	}
	sig, ok := tv.Type.Underlying().(*types.Signature)
	if !ok {
		fail(el.start, "<%s> isn't a component: a component is a function returning a vuka.Node", el.tag)
		return
	}
	node := e.nodeType()
	if node == nil {
		c.fail = "the runtime has no Node"
		return
	}
	var res types.Type
	switch r := sig.Results(); {
	case r.Len() == 1:
		res = r.At(0).Type()
	case r.Len() == 2 && types.Identical(r.At(1).Type(), errorType):
		res, c.try = r.At(0).Type(), true
	default:
		fail(el.start, "<%s> must return a vuka.Node, or a Node and an error", el.tag)
		return
	}
	if !types.AssignableTo(res, node) {
		fail(el.start, "<%s> returns %s, which isn't a vuka.Node: it has no Render(context.Context, io.Writer) error method", el.tag, e.display(res))
		return
	}
	var attrs []*jsxAttr
	for _, a := range el.attrs {
		if a.name != "key" {
			attrs = append(attrs, a)
		}
	}
	hasKids := len(el.kids) > 0
	noKids := func() bool {
		if isTemplComponent(res, node) {
			c.withKids = true
			return true
		}
		fail(el.start, "<%s> takes no children", el.tag)
		return false
	}

	params := sig.Params()
	if st, typ := propsStruct(sig); st != nil {
		c.props = e.typeTextAuto(f, typ)
		var names []string
		for i := 0; i < st.NumFields(); i++ {
			names = append(names, st.Field(i).Name())
		}
		used := map[int]bool{}
		for _, a := range attrs {
			i := bindName(names, a.name)
			if i < 0 {
				fail(a.off, "%s", unknownAttr(el.tag, a.name, "fields", names))
				return
			}
			if used[i] {
				fail(a.off, "<%s> sets %s twice", el.tag, names[i])
				return
			}
			used[i] = true
			c.args = append(c.args, &compArg{key: names[i] + ": ", attr: a})
		}
		if hasKids {
			if i := bindName(names, "Children"); i >= 0 && !used[i] && types.AssignableTo(node, st.Field(i).Type()) {
				c.args = append(c.args, &compArg{key: names[i] + ": ", kids: true})
			} else if !noKids() {
				return
			}
		}
		if sig.Variadic() && len(c.args) == 0 {
			c.props = ""
		}
		c.resolved = true
		e.progress = true
		return
	}

	var names []string
	for i := 0; i < params.Len(); i++ {
		names = append(names, params.At(i).Name())
	}
	args := make([]*compArg, params.Len())
	for _, a := range attrs {
		i := bindName(names, a.name)
		if i < 0 {
			fail(a.off, "%s", unknownAttr(el.tag, a.name, "parameters", names))
			return
		}
		if args[i] != nil {
			fail(a.off, "<%s> sets %s twice", el.tag, names[i])
			return
		}
		args[i] = &compArg{attr: a}
	}
	if hasKids {
		i := bindName(names, "children")
		switch {
		case i >= 0 && args[i] == nil && types.AssignableTo(node, params.At(i).Type()):
			args[i] = &compArg{kids: true}
		case !noKids():
			return
		}
	}
	last := -1
	inOrder := true
	for i, a := range args {
		switch {
		case a == nil && sig.Variadic() && i == len(args)-1:
			continue
		case a == nil:
			z, msg := e.zero(f, params.At(i).Type())
			if msg != "" {
				z = "*new(" + e.typeTextAuto(f, params.At(i).Type()) + ")"
			}
			a = &compArg{text: z}
		case a.kids:
			inOrder = inOrder && last < el.end
			last = el.end
		case a.attr.kind == 'e':
			inOrder = inOrder && last < a.attr.expr.start
			last = a.attr.expr.start
		}
		a.typ = params.At(i).Type()
		if sig.Variadic() && i == len(args)-1 {
			a.typ = a.typ.(*types.Slice).Elem()
		}
		c.args = append(c.args, a)
	}
	if !inOrder {
		e.closeOver(f, c)
	}
	c.resolved = true
	e.progress = true
}

// closeOver binds a component's expressions out of order by passing them, in
// source order, to a function literal that makes the call, so each stays in
// place: func(__a1 bool, __a2 User) vuka.Node { return Card(__a2, __a1) }(x, u).
func (e *engine) closeOver(f *fileState, c *jsxComp) {
	type item struct {
		a   *compArg
		off int
		typ string
	}
	var items []item
	for _, a := range c.args {
		switch {
		case a.kids:
			items = append(items, item{a, c.el.end, f.rt + ".Node"})
		case a.attr != nil && a.attr.kind == 'e':
			items = append(items, item{a, a.attr.expr.start, e.typeTextAuto(f, a.typ)})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].off < items[j].off })
	for i, it := range items {
		it.a.name = "__a" + itoa(i+1)
		c.closure = append(c.closure, it.a)
		c.closureTyp = append(c.closureTyp, it.typ)
	}
}

// propsStruct is the struct a component takes as its only parameter, or as a
// variadic of one struct type (templUI's func Button(props ...ButtonProps)).
func propsStruct(sig *types.Signature) (*types.Struct, types.Type) {
	if sig.Params().Len() != 1 || sig.Params().At(0).Name() == "children" {
		return nil, nil
	}
	t := sig.Params().At(0).Type()
	if sig.Variadic() {
		t = t.(*types.Slice).Elem()
	}
	st, _ := t.Underlying().(*types.Struct)
	if st == nil {
		return nil, nil
	}
	return st, t
}

// bindName finds an attribute's parameter or field: the same name, or one
// differing only in its first letter's case (user ↔ User).
func bindName(names []string, attr string) int {
	for i, n := range names {
		if n == attr {
			return i
		}
	}
	for i, n := range names {
		if n != "" && n != "_" && strings.EqualFold(n[:1], attr[:1]) && n[1:] == attr[1:] {
			return i
		}
	}
	return -1
}

func unknownAttr(tag, attr, what string, names []string) string {
	var known []string
	for _, n := range names {
		if n != "" && n != "_" {
			known = append(known, n)
		}
	}
	if s := closestName(attr, known); s != "" {
		return "<" + tag + "> has no attribute " + attr + "; did you mean " + s + "?"
	}
	if len(known) == 0 {
		return "<" + tag + "> takes no attributes"
	}
	return "<" + tag + "> has no attribute " + attr + "; its " + what + " are " + strings.Join(known, ", ")
}

// closestName is the name nearest to s by edit distance, if any is near.
func closestName(s string, names []string) string {
	best, bestD := "", 3
	for _, n := range names {
		if d := editDistance(strings.ToLower(s), strings.ToLower(n)); d < bestD && d < len(n) {
			best, bestD = n, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// typeTextAuto writes t for f, importing under a name of its own any package f
// doesn't import.
func (e *engine) typeTextAuto(f *fileState, t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		missing := ""
		q := e.qualifier(f, &missing)(p)
		if missing != "" {
			return f.autoImport(p)
		}
		return q
	})
}

// importAs imports path into f as name, once.
func (f *fileState) importAs(path, name string) string {
	if f.autoImports == nil {
		f.autoImports = map[string]bool{}
	}
	if !f.autoImports[path] {
		f.autoImports[path] = true
		f.insert(f.pkgEnd, "; import "+name+" "+strconv.Quote(path), 0)
	}
	return name
}
