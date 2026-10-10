package transpile

import (
	"go/ast"
	"go/constant"
	"go/token"
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
	propsTyp   types.Type // the props struct's, until written
	args       []*compArg // in the call's order
	try        bool       // the function returns (Node, error)
	withKids   bool       // children go through templx.WithChildren
	inOrder    bool       // its expressions are in the call's order
	closure    []*compArg // when bound out of order: the arguments taken by a function literal, in source order
	closureTyp []string
	stateful   bool     // a struct embedding vuka.Live: vuka.Component(site, key, &T{…})
	site       string   // the tag's place, for a stateful component
	key        *compArg // its key attribute
}

// nestStep is an embedded struct field a promoted field is reached through.
type nestStep struct {
	name string
	typ  types.Type // the embedded field's type, a pointer's element
	ptr  bool
	text string
}

// compArg is one argument of a component call.
type compArg struct {
	key  string   // "Field: " for a props struct
	attr *jsxAttr // the value of an attribute
	kids bool     // the children, as a Fragment
	zero bool     // the parameter is left out
	text string   // its zero value
	name string   // the function literal's parameter, in the closure form
	typ  types.Type
	nest []nestStep // the embedded fields a promoted props field is in
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
		w.kids(el.kids)
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
			w.frame(el.tagEnd, el.kids)
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
	if c.stateful {
		w.gen(rt + ".Component(" + strconv.Quote(c.site) + ", ")
		if c.key != nil {
			value(c.key, true)
		} else {
			w.gen("nil")
		}
		w.gen(", &")
		w.keep(tag)
		w.gen("{")
	} else {
		w.keep(tag)
		w.gen("(")
		if c.props != "" {
			w.gen(c.props + "{")
		}
	}
	var open []nestStep
	closeTo := func(k int) {
		for ; len(open) > k; open = open[:len(open)-1] {
			w.gen("}, ")
		}
	}
	for _, a := range c.args {
		k := 0
		for k < len(open) && k < len(a.nest) && open[k].name == a.nest[k].name {
			k++
		}
		closeTo(k)
		for _, st := range a.nest[k:] {
			w.gen(st.name + ": ")
			if st.ptr {
				w.gen("&")
			}
			w.gen(st.text + "{")
			open = append(open, st)
		}
		w.gen(a.key)
		value(a, true)
		w.gen(", ")
	}
	closeTo(0)
	switch {
	case c.stateful:
		w.gen("})")
	case c.props != "":
		w.gen("})")
	default:
		w.gen(")")
	}
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
// A generic function's type arguments are inferred as a call's are; an
// overloaded function's overload is the one its attributes fit best.
func (e *engine) resolveComp(f *fileState, c *jsxComp) {
	el := c.el
	at, end := f.cur.fromOrig(el.start+1), f.cur.fromOrig(el.tagEnd)
	var expr ast.Expr
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if x, ok := n.(ast.Expr); ok && expr == nil && f.off(x.Pos()) == at && f.off(x.End()) == end {
			switch x.(type) {
			case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr:
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
	node := e.nodeType()
	if node == nil {
		c.fail = "the runtime has no Node"
		return
	}
	var attrs []*jsxAttr
	for _, a := range el.attrs {
		if a.name != "key" {
			attrs = append(attrs, a)
		}
	}
	if set, id := e.target(expr); set != nil {
		e.resolveOverload(f, c, set, id, attrs, node, fail)
		return
	}
	obj := e.info.Uses[compIdent(expr)]
	if tn, ok := obj.(*types.TypeName); ok {
		e.resolveStateful(f, c, expr, tn, attrs, node, fail)
		return
	}
	var sig *types.Signature
	if tv, ok := e.info.Types[expr]; ok && !isInvalid(tv.Type) {
		if sig, ok = tv.Type.Underlying().(*types.Signature); !ok {
			fail(el.start, "<%s> isn't a component: a component is a function returning a vuka.Node", el.tag)
			return
		}
	}
	if fn, ok := obj.(*types.Func); ok && fn.Signature().TypeParams().Len() > 0 && (sig == nil || sig.TypeParams().Len() > 0) {
		var off int
		var msg string
		if sig, off, msg = e.instantiate(f, c, expr, fn.Signature(), attrs, node); msg != "" {
			fail(off, "%s", msg)
			return
		}
		if sig == nil {
			c.fail = "an attribute's type is unknown"
			return
		}
	}
	if sig == nil {
		c.fail = "its type is unknown"
		return
	}
	if off, msg := e.bindComp(c, sig, attrs, node, e.templChildren(obj)); msg != "" {
		fail(off, "%s", msg)
		return
	}
	e.finishComp(f, c)
}

// compIdent is the name a component's tag ends in: Card in <theme.Card> and <Card[T]>.
func compIdent(x ast.Expr) *ast.Ident {
	switch x := x.(type) {
	case *ast.Ident:
		return x
	case *ast.SelectorExpr:
		return x.Sel
	case *ast.IndexExpr:
		return compIdent(x.X)
	case *ast.IndexListExpr:
		return compIdent(x.X)
	}
	return nil
}

// templChildren reports whether a component may read its children from templ's
// context: a function declared in a .templ file, one of another package (a
// templ library, perhaps), or a function value. A function of this package's
// .vuka and .go files never does.
func (e *engine) templChildren(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	if !ok || fn.Pkg() != e.pkg {
		return true
	}
	file := e.fileOf(fn.Pos())
	return file != nil && strings.HasSuffix(file.name, "_templ.go")
}

// bindComp binds a component's attributes and children to the parameters (or
// props fields) of sig, or says why they don't fit and where.
func (e *engine) bindComp(c *jsxComp, sig *types.Signature, attrs []*jsxAttr, node types.Type, templKids bool) (int, string) {
	el := c.el
	var res types.Type
	switch r := sig.Results(); {
	case r.Len() == 1:
		res = r.At(0).Type()
	case r.Len() == 2 && types.Identical(r.At(1).Type(), errorType):
		res, c.try = r.At(0).Type(), true
	default:
		return el.start, "<" + el.tag + "> must return a vuka.Node, or a Node and an error"
	}
	if !types.AssignableTo(res, node) {
		return el.start, "<" + el.tag + "> returns " + e.display(res) + ", which isn't a vuka.Node: it has no Render(context.Context, io.Writer) error method"
	}
	hasKids := len(el.kids) > 0
	noKids := func(add string) string {
		switch {
		case !isTemplComponent(res, node):
			return "<" + el.tag + "> takes no children"
		case templKids:
			c.withKids = true
			return ""
		}
		return "<" + el.tag + "> takes no children: add a " + add
	}

	params := sig.Params()
	if st, typ := propsStruct(sig, attrs); st != nil {
		c.propsTyp, c.inOrder = typ, true
		kids, off, msg := e.bindFields(c, st, attrs, node, false)
		if msg != "" {
			return off, msg
		}
		if hasKids && !kids {
			if msg := noKids("Children vuka.Node field to " + e.display(typ)); msg != "" {
				return el.start, msg
			}
		}
		if sig.Variadic() && len(c.args) == 0 {
			c.propsTyp = nil
		}
		return 0, ""
	}

	var names []string
	for i := 0; i < params.Len(); i++ {
		names = append(names, params.At(i).Name())
	}
	args := make([]*compArg, params.Len())
	for _, a := range attrs {
		i := bindName(names, a.name)
		if i < 0 {
			return a.off, unknownAttr(el.tag, a.name, "parameters", names)
		}
		if args[i] != nil {
			return a.off, "<" + el.tag + "> sets " + names[i] + " twice"
		}
		args[i] = &compArg{attr: a}
	}
	if hasKids {
		i := bindName(names, "children")
		switch {
		case i >= 0 && args[i] == nil && types.AssignableTo(node, params.At(i).Type()):
			args[i] = &compArg{kids: true}
		default:
			if msg := noKids("children vuka.Node parameter"); msg != "" {
				return el.start, msg
			}
		}
	}
	last := -1
	c.inOrder = true
	for i, a := range args {
		switch {
		case a == nil && sig.Variadic() && i == len(args)-1:
			continue
		case a == nil:
			a = &compArg{zero: true}
		case a.kids:
			c.inOrder = c.inOrder && last < el.end
			last = el.end
		case a.attr.kind == 'e':
			c.inOrder = c.inOrder && last < a.attr.expr.start
			last = a.attr.expr.start
		}
		a.typ = params.At(i).Type()
		if sig.Variadic() && i == len(args)-1 {
			a.typ = a.typ.(*types.Slice).Elem()
		}
		c.args = append(c.args, a)
	}
	return 0, ""
}

// finishComp writes what a bound component's call needs: its props type, its
// zero values, and the closure that keeps its expressions in source order.
func (e *engine) finishComp(f *fileState, c *jsxComp) {
	if c.propsTyp != nil && !c.stateful {
		c.props = e.typeTextAuto(f, c.propsTyp)
	}
	if c.propsTyp != nil {
		c.args = groupArgs(c.args, 0)
		last := -1
		in := func(a *compArg) {
			switch {
			case a == nil:
			case a.kids:
				c.inOrder = c.inOrder && last < c.el.end
				last = c.el.end
			case a.attr != nil && a.attr.kind == 'e':
				c.inOrder = c.inOrder && last < a.attr.expr.start
				last = a.attr.expr.start
			}
		}
		c.inOrder = true
		in(c.key)
		for _, a := range c.args {
			in(a)
			for i := range a.nest {
				a.nest[i].text = e.typeTextAuto(f, a.nest[i].typ)
			}
		}
		if c.stateful && c.key != nil && c.key.attr.kind == 'e' {
			c.inOrder = false // vuka.Component(site, key, &T{…}) writes the key before the tag
		}
	}
	for _, a := range c.args {
		if a.zero {
			z, msg := e.zero(f, a.typ)
			if msg != "" {
				z = "*new(" + e.typeTextAuto(f, a.typ) + ")"
			}
			a.text = z
		}
	}
	if !c.inOrder {
		e.closeOver(f, c)
	}
	c.resolved = true
	e.progress = true
}

// attrExpr is an attribute's expression in this round's syntax tree.
func (e *engine) attrExpr(f *fileState, a *jsxAttr) ast.Expr {
	at, end := f.cur.fromOrig(a.expr.start), f.cur.fromOrig(a.expr.end)
	var expr ast.Expr
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if x, ok := n.(ast.Expr); ok && expr == nil && f.off(x.Pos()) == at && f.off(x.End()) == end {
			expr = x
		}
		return expr == nil && (n == nil || f.off(n.Pos()) <= at)
	})
	return expr
}

// instantiate infers a generic component's type arguments as Go infers a
// call's, from the call its tag makes, where a parameter it leaves out takes
// nil and so infers nothing. It returns nil while an attribute's type is unknown.
func (e *engine) instantiate(f *fileState, c *jsxComp, fun ast.Expr, sig *types.Signature, attrs []*jsxAttr, node types.Type) (*types.Signature, int, string) {
	el := c.el
	if st, _ := propsStruct(sig, attrs); st != nil {
		return nil, el.start, "<" + el.tag + "> takes a generic props struct, whose type arguments can't be inferred: write <" + el.tag + "[…]>"
	}
	trial := &jsxComp{el: el}
	if off, msg := e.bindComp(trial, sig, attrs, node, true); msg != "" {
		return nil, off, msg
	}
	pos := fun.Pos()
	call := &ast.CallExpr{Fun: fun, Lparen: fun.End(), Rparen: fun.End()}
	for _, a := range trial.args {
		var x ast.Expr = &ast.Ident{NamePos: pos, Name: "nil"}
		switch {
		case a.attr == nil:
		case a.attr.kind == 's':
			x = &ast.BasicLit{ValuePos: pos, Kind: token.STRING, Value: strconv.Quote(a.attr.val)}
		case a.attr.kind == 'b':
			x = &ast.Ident{NamePos: pos, Name: "true"}
		default:
			x = e.attrExpr(f, a.attr)
			if tv, ok := e.info.Types[x]; x == nil || !ok || isInvalid(tv.Type) {
				return nil, 0, ""
			}
		}
		call.Args = append(call.Args, x)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Instances: map[*ast.Ident]types.Instance{}}
	_ = types.CheckExpr(e.fset, e.pkg, pos, call, info) // a mismatch is reported where the call is made
	if inst, ok := info.Instances[compIdent(fun)]; ok {
		if s, ok := inst.Type.(*types.Signature); ok && s.TypeParams().Len() == 0 {
			return s, 0, ""
		}
	}
	return nil, el.start, "<" + el.tag + "> is generic, and its attributes don't give its type arguments: write <" + el.tag + "[…]>"
}

// resolveOverload chooses the overload of a component whose parameters fit
// its attributes best, scored as a call's arguments are.
func (e *engine) resolveOverload(f *fileState, c *jsxComp, set *overloadSet, id *ast.Ident, attrs []*jsxAttr, node types.Type, fail func(int, string, ...any)) {
	best := -1
	var winners []*overload
	var bound []*jsxComp
	for _, o := range set.list {
		if o.sig == nil {
			continue
		}
		b := &jsxComp{el: c.el}
		if _, msg := e.bindComp(b, o.sig, attrs, node, false); msg != "" {
			continue
		}
		score, ok, known := e.compScore(f, b, node)
		switch {
		case !known:
			c.fail = "an attribute's type is unknown"
			return
		case !ok:
		case best < 0 || score < best:
			best, winners, bound = score, []*overload{o}, []*jsxComp{b}
		case score == best:
			winners, bound = append(winners, o), append(bound, b)
		}
	}
	switch len(winners) {
	case 0:
		fail(c.el.start, "no overload of %s takes these attributes%s", set.display(), e.candidates(set.list))
		return
	case 1:
	default:
		fail(c.el.start, "<%s> is ambiguous: its attributes fit%s", c.el.tag, e.candidates(winners))
		return
	}
	*c = *bound[0]
	off := f.orig(id.Pos())
	f.add(off, off+len(id.Name), winners[0].mangled)
	f.done[off] = true
	e.finishComp(f, c)
}

// compScore scores a component's binding as pick scores a call's arguments.
// It reports whether every attribute fits, and whether their types are known.
func (e *engine) compScore(f *fileState, c *jsxComp, node types.Type) (score int, ok, known bool) {
	for _, a := range c.args {
		var x arg
		switch {
		case a.kids:
			x = arg{t: node}
		case a.attr == nil:
			continue
		case a.attr.kind == 's':
			x = arg{types.Typ[types.UntypedString], constant.MakeString(a.attr.val)}
		case a.attr.kind == 'b':
			x = arg{types.Typ[types.UntypedBool], constant.MakeBool(true)}
		default:
			ex := e.attrExpr(f, a.attr)
			tv, found := e.info.Types[ex]
			if ex == nil || !found || isInvalid(tv.Type) {
				return 0, false, false
			}
			x = arg{tv.Type, tv.Value}
			if lit, ok := ast.Unparen(ex).(*ast.BasicLit); ok {
				x.t = untypedLit[lit.Kind] // its default type when it went into the placeholder
			}
		}
		s, fits := argScore(x, a.typ)
		if !fits {
			return 0, false, true
		}
		score += s
	}
	return score, true, true
}

var untypedLit = map[token.Token]types.Type{
	token.INT: types.Typ[types.UntypedInt], token.FLOAT: types.Typ[types.UntypedFloat],
	token.IMAG: types.Typ[types.UntypedComplex], token.CHAR: types.Typ[types.UntypedRune],
	token.STRING: types.Typ[types.UntypedString],
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
	if k := c.key; k != nil && k.attr.kind == 'e' {
		items = append(items, item{k, k.attr.expr.start, e.typeTextAuto(f, k.typ)})
	}
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
// An attribute naming the parameter itself (<Row user={u}/> for func
// Row(user User)) passes the struct whole instead, unless a field has that name.
func propsStruct(sig *types.Signature, attrs []*jsxAttr) (*types.Struct, types.Type) {
	if sig.Params().Len() != 1 || sig.Params().At(0).Name() == "children" {
		return nil, nil
	}
	param := sig.Params().At(0)
	t := param.Type()
	if sig.Variadic() {
		t = t.(*types.Slice).Elem()
	}
	st, _ := t.Underlying().(*types.Struct)
	if st == nil {
		return nil, nil
	}
	if !sig.Variadic() {
		var fields []string
		for i := 0; i < st.NumFields(); i++ {
			fields = append(fields, st.Field(i).Name())
		}
		for _, a := range attrs {
			if bindName([]string{param.Name()}, a.name) >= 0 && bindName(fields, a.name) < 0 {
				return nil, nil
			}
		}
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
