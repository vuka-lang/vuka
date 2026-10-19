package transpile

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/diag"
)

const templPath = "github.com/a-h/templ"

// jsxComp is a component tag, <UserCard user={u} admin />: a call of the Go
// function it names, decided once that function's type is known. Until then
// the tag lowers to a placeholder that type-checks its expressions in place.
type jsxComp struct {
	el         *jsxElem
	target     *jsxTarget
	resolved   bool
	dead       bool
	fail       string
	props      string     // the props struct literal's type; "" when attributes are parameters
	propsTyp   types.Type // the props struct's, until written
	args       []*compArg // in the call's order
	try        bool       // the function returns (Node, error)
	withKids   bool       // children go through the target's WithChildren
	inOrder    bool       // its expressions are in the call's order
	closure    []*compArg // when bound out of order: the arguments taken by a function literal, in source order
	closureTyp []string
	stateful   bool     // a struct whose pointer is the target's Stateful: ui.Component(site, key, &T{…})
	key        *compArg // its key attribute
	targText   string   // inferred type arguments for a generic stateful literal, e.g. "[int]"; "" otherwise

	// UI islands (Phase 1): a <ui.Island> wrapper lowered to ui.Mount.
	island                        bool      // this comp is a <ui.Island> wrapper
	consumed                      bool      // a tag a <ui.Island> absorbs (its <ui.Template>s and body): not resolved or emitted on its own
	bodyTag                       span      // the island body component's type-name span (kept in the direct form)
	islandStrat                   string    // the strategy constructor: Load/Idle/Visible/Media/ClientOnly/SSR; "" = none
	strat                         *jsxAttr  // the client:*/server:defer attribute, for its value
	islandArgTyp                  []string  // closure-mode param type text, one per args entry
	slotKids                      []jsxNode // the body's children → ui.Slot (when any)
	fallbackKids                  []jsxNode // the <ui.Template fallback>'s children → ui.Fallback (when present)
	hasSlot                       bool
	hasFallback                   bool
	islandEmits                   []islandEmit // on<Emit> handler attributes → ui.Emit(name, handler)
	islandFuncs                   []islandProp // function props → ui.Callback(prop, fn)
	islandStates                  []islandProp // state props → ui.StateProp(prop, state)
	islandOnce                    bool         // once → ui.Once()
	islandFuncTyp, islandStateTyp []string     // their closure-mode parameter types
}

// islandEmit is one on<Emit> binding on an island body tag: the emit name
// (the attribute name minus the leading "on", first letter lower-cased) and the
// handler expression attribute, emitted verbatim as ui.Emit's handler arg.
type islandEmit struct {
	name string
	attr *jsxAttr
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
	wrap string     // a function the value is passed through: an island's ui.Ptr[T]
	conv string     // a conversion of the value: an island's number prop takes any Go number
}

func (w *jsxWriter) comp(c *jsxComp) {
	if c.island {
		w.island(c)
		return
	}
	el, q := c.el, w.q
	tag := span{el.start + 1, el.tagEnd}
	if !c.resolved {
		w.gen(q + "Fragment(" + q + "Child(")
		w.keep(tag)
		w.gen("), ")
		for _, a := range el.attrs {
			if a.kind == 'e' {
				w.gen(q + "Child(")
				w.keep(a.expr)
				w.gen("), ")
			}
		}
		if len(el.kids) > 0 {
			w.piece(el.kids)
		}
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
			w.piece(el.kids)
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
		res := q + "Node"
		if c.stateful && c.key == nil {
			res = "any"
		}
		w.gen("func(" + strings.Join(params, ", ") + ") " + res + " { return ")
	}
	if c.withKids {
		w.gen(q + "WithChildren(")
	}
	if c.try {
		w.gen(q + "Try(")
	}
	if c.stateful {
		if c.key != nil {
			w.gen(q + "Key(")
			value(c.key, true)
			w.gen(", ")
		}
		w.gen("&")
		if c.key != nil {
			w.copy(tag) // ui.Key(key, &T{…}) writes the key before the tag
		} else {
			w.keep(tag)
		}
		w.gen(c.targText) // "" normally; "[int]" for an inferred generic stateful literal
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
	case c.stateful && c.key != nil:
		w.gen("})")
	case c.stateful:
		w.gen("}")
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
		if all && e.bindTree(f, t) {
			f.fixed = append(f.fixed, f.jsxEdits(t, true)...)
			t.done = true
			e.progress = true
		}
	}
}

// nodeType is the target's Node.
func (e *engine) nodeType(t *jsxTarget) types.Type {
	if obj, ok := e.targetObj(t, "Node").(*types.TypeName); ok {
		return obj.Type()
	}
	return nil
}

// resolveComp decides the call a component tag makes:
//
//	<UserCard user={u} admin>hi</UserCard>   →   UserCard(u, true, userCardText.Render())
//	<ui.Button variant={v} />                →   ui.Button(ui.ButtonProps{Variant: v, })
//
// Attributes bind to parameters by name (or, for a function taking one props
// struct, to its fields); missing ones get zero values, children go to a
// children parameter, else — for a component that may read them from its
// context — through the target's WithChildren.
// A generic function's type arguments are inferred as a call's are; an
// overloaded function's overload is the one its attributes fit best.
// compExpr is a component tag's expression in this round's syntax tree: the
// Ident, selector or index placeholder lowerJSX left where the tag name was.
func (e *engine) compExpr(f *fileState, c *jsxComp) ast.Expr {
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
	return expr
}

func (e *engine) resolveComp(f *fileState, c *jsxComp) {
	if c.consumed { // a <ui.Island> absorbed this tag; the wrapper emits it
		c.resolved = true
		return
	}
	el := c.el
	expr := e.compExpr(f, c)
	if expr == nil {
		c.fail = "internal: placeholder not found"
		return
	}
	fail := func(code diag.Code, off int, format string, args ...any) {
		c.dead = true
		e.errs.add(code, f.at(off), format, args...)
	}
	// UI islands: a <ui.Island> wrapper, and a <ui.Template> outside one.
	switch e.targetFunc(c.target, e.info.Uses[compIdent(expr)]) {
	case "Island":
		e.resolveIsland(f, c, fail)
		return
	case "Template":
		fail("E0306", el.start, "<%s> is only valid directly inside <ui.Island>", el.tag)
		return
	}
	node := e.nodeType(c.target)
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
	if tn, ok := obj.(*types.TypeName); ok && e.isIslandType(tn) {
		if e.bindIsland(f, c, el, fail) {
			e.finishIsland(f, c)
		}
		return
	}
	if tn, ok := obj.(*types.TypeName); ok {
		e.resolveStateful(f, c, expr, tn, attrs, node, fail)
		return
	}
	var sig *types.Signature
	if tv, ok := e.info.Types[expr]; ok && !isInvalid(tv.Type) {
		if sig, ok = tv.Type.Underlying().(*types.Signature); !ok {
			fail("E0106", el.start, "<%s> isn't a component: a component is a function returning %sNode", el.tag, c.target.q)
			return
		}
	}
	if fn, ok := obj.(*types.Func); ok && fn.Signature().TypeParams().Len() > 0 && (sig == nil || sig.TypeParams().Len() > 0) {
		var off int
		var msg string
		if sig, off, msg = e.instantiate(f, c, expr, fn.Signature(), attrs, node); msg != "" {
			fail("E0106", off, "%s", msg)
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
		fail("E0106", off, "%s", msg)
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
		return el.start, "<" + el.tag + "> must return " + c.target.q + "Node, or one and an error"
	}
	if !types.AssignableTo(res, node) {
		msg := "<" + el.tag + "> returns " + e.display(res) + ", which isn't " + c.target.q + "Node"
		if iface, ok := node.Underlying().(*types.Interface); ok {
			if m, wrong := types.MissingMethod(res, iface, true); m != nil && !wrong {
				msg += " (missing method " + m.Name() + ")"
			} else if m != nil {
				msg += " (wrong type for method " + m.Name() + ")"
			}
		}
		return el.start, msg
	}
	hasKids := len(el.kids) > 0
	noKids := func(add string) string {
		switch {
		case !types.Identical(res, node) || !c.target.has("WithChildren"):
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
			if msg := noKids("Children " + c.target.q + "Node field to " + e.display(typ)); msg != "" {
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
			if msg := noKids("children " + c.target.q + "Node parameter"); msg != "" {
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
	if c.target.has("UseState") {
		for _, a := range c.args {
			e.bindArg(f, c, a)
		}
	}
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
		return e.inferProps(f, c, sig, st, attrs, node)
	}
	trial := &jsxComp{el: el, target: c.target}
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

// inferProps infers a generic component's type arguments when its single
// parameter is a generic props struct, which Go's call inference can't reach
// because the struct is written as a literal, not passed as arguments. Each
// attribute binds to a field whose declared type mentions the type parameters
// (Rows []T, Cell func(T) string); unifying that against the attribute value's
// concrete type pins the parameters. It returns nil while an attribute's type
// is still unknown, so the next round can retry.
func (e *engine) inferProps(f *fileState, c *jsxComp, sig *types.Signature, st *types.Struct, attrs []*jsxAttr, node types.Type) (*types.Signature, int, string) {
	el := c.el
	trial := &jsxComp{el: el, target: c.target}
	if off, msg := e.bindComp(trial, sig, attrs, node, true); msg != "" {
		return nil, off, msg
	}
	targs, u, status := e.solveTargs(f, trial.args, sig.TypeParams())
	switch status {
	case inferRetry:
		return nil, 0, "" // a type is still unknown; retry next round
	case inferConflict:
		return nil, el.start, conflictMsg(el.tag, u, e)
	case inferUnder:
		return nil, el.start, underMsg(el.tag)
	}
	inst, err := types.Instantiate(nil, sig, targs, true)
	if err != nil {
		return nil, el.start, "<" + el.tag + ">'s inferred type arguments don't fit its constraints: write <" + el.tag + "[…]> (" + err.Error() + ")"
	}
	if s, ok := inst.(*types.Signature); ok {
		return s, 0, ""
	}
	return nil, el.start, underMsg(el.tag)
}

// inferStatus is how solveTargs ended: solved, waiting on an unknown type, a
// parameter bound two ways, or a parameter left unbound.
type inferStatus int

const (
	inferOK inferStatus = iota
	inferRetry
	inferConflict
	inferUnder
)

// attrConc is an attribute value's concrete type for inference: string for a
// string attribute, bool for a boolean one, else the attribute expression's
// default type. ok is false while that expression's type isn't known yet, so
// the caller returns the retry sentinel and the fixpoint loop tries again.
func (e *engine) attrConc(f *fileState, a *jsxAttr) (conc types.Type, ok bool) {
	switch a.kind {
	case 's':
		return types.Typ[types.String], true
	case 'b':
		return types.Typ[types.Bool], true
	default:
		x := e.attrExpr(f, a)
		tv, found := e.info.Types[x]
		if x == nil || !found || isInvalid(tv.Type) {
			return nil, false
		}
		return types.Default(tv.Type), true
	}
}

// solveTargs pins tps by unifying each bound attribute's declared type (which
// still mentions the type parameters) against its value's concrete type, the
// shared core of both inferProps (function components) and inferStateful
// (struct components). args come from a trial bind; children and left-out
// fields have no attr and infer nothing.
func (e *engine) solveTargs(f *fileState, args []*compArg, tps *types.TypeParamList) ([]types.Type, *unifier, inferStatus) {
	u := &unifier{tps: tps, subst: map[*types.TypeParam]types.Type{}}
	for _, a := range args {
		if a.attr == nil {
			continue
		}
		conc, ok := e.attrConc(f, a.attr)
		if !ok {
			return nil, u, inferRetry
		}
		if !u.unify(a.typ, conc) {
			return nil, u, inferConflict
		}
	}
	targs := make([]types.Type, tps.Len())
	for i := range targs {
		t, ok := u.subst[tps.At(i)]
		if !ok {
			return nil, u, inferUnder
		}
		targs[i] = t
	}
	return targs, u, inferOK
}

func conflictMsg(tag string, u *unifier, e *engine) string {
	return "<" + tag + "> is generic, and its attributes make " + u.bad.Obj().Name() +
		" both " + e.display(u.was) + " and " + e.display(u.now) + ": write <" + tag + "[…]>"
}

func underMsg(tag string) string {
	return "<" + tag + "> is generic, and its attributes don't give its type arguments: write <" + tag + "[…]>"
}

// unifier accumulates type-parameter bindings while matching props fields'
// declared types against attribute values' concrete types. go/types has no
// exported general unifier, so this one covers the field shapes props use.
type unifier struct {
	tps      *types.TypeParamList
	subst    map[*types.TypeParam]types.Type
	bad      *types.TypeParam // the first parameter bound two incompatible ways
	was, now types.Type
}

// unify matches a field's declared type pat (which may mention the type
// parameters) against an attribute value's concrete type conc, recording each
// parameter's binding. A shape the two don't share contributes nothing — not
// every field pins a parameter — so it returns false only on a genuine
// conflict: a parameter already bound to an incompatible type.
func (u *unifier) unify(pat, conc types.Type) bool {
	if tp, ok := types.Unalias(pat).(*types.TypeParam); ok && u.has(tp) {
		switch prev, ok := u.subst[tp]; {
		case !ok:
			u.subst[tp] = conc
		case !types.Identical(prev, conc):
			u.bad, u.was, u.now = tp, prev, conc
			return false
		}
		return true
	}
	switch p := pat.Underlying().(type) {
	case *types.Slice:
		if c, ok := conc.Underlying().(*types.Slice); ok {
			return u.unify(p.Elem(), c.Elem())
		}
	case *types.Array:
		if c, ok := conc.Underlying().(*types.Array); ok {
			return u.unify(p.Elem(), c.Elem())
		}
	case *types.Pointer:
		if c, ok := conc.Underlying().(*types.Pointer); ok {
			return u.unify(p.Elem(), c.Elem())
		}
	case *types.Map:
		if c, ok := conc.Underlying().(*types.Map); ok {
			return u.unify(p.Key(), c.Key()) && u.unify(p.Elem(), c.Elem())
		}
	case *types.Chan:
		if c, ok := conc.Underlying().(*types.Chan); ok {
			return u.unify(p.Elem(), c.Elem())
		}
	case *types.Signature:
		if c, ok := conc.Underlying().(*types.Signature); ok {
			return u.unifyTuple(p.Params(), c.Params()) && u.unifyTuple(p.Results(), c.Results())
		}
	}
	// A named generic type N[X,…] (Column[T]): unify its arguments arg-wise.
	if pn, ok := types.Unalias(pat).(*types.Named); ok && pn.TypeArgs().Len() > 0 {
		if cn, ok := types.Unalias(conc).(*types.Named); ok &&
			pn.TypeArgs().Len() == cn.TypeArgs().Len() && types.Identical(pn.Origin(), cn.Origin()) {
			for i := 0; i < pn.TypeArgs().Len(); i++ {
				if !u.unify(pn.TypeArgs().At(i), cn.TypeArgs().At(i)) {
					return false
				}
			}
		}
	}
	return true // an unsupported shape simply contributes no binding
}

func (u *unifier) unifyTuple(p, c *types.Tuple) bool {
	if p.Len() != c.Len() {
		return true // a shape mismatch contributes nothing rather than conflicting
	}
	for i := 0; i < p.Len(); i++ {
		if !u.unify(p.At(i).Type(), c.At(i).Type()) {
			return false
		}
	}
	return true
}

func (u *unifier) has(tp *types.TypeParam) bool {
	for i := 0; i < u.tps.Len(); i++ {
		if u.tps.At(i) == tp {
			return true
		}
	}
	return false
}

// resolveOverload chooses the overload of a component whose parameters fit
// its attributes best, scored as a call's arguments are.
func (e *engine) resolveOverload(f *fileState, c *jsxComp, set *overloadSet, id *ast.Ident, attrs []*jsxAttr, node types.Type, fail func(diag.Code, int, string, ...any)) {
	best := -1
	var winners []*overload
	var bound []*jsxComp
	for _, o := range set.list {
		if o.sig == nil {
			continue
		}
		b := &jsxComp{el: c.el, target: c.target}
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
		fail("E0106", c.el.start, "no overload of %s takes these attributes%s", set.display(), e.candidates(set.list))
		return
	case 1:
	default:
		fail("E0121", c.el.start, "<%s> is ambiguous: its attributes fit%s", c.el.tag, e.candidates(winners))
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
// place: func(__a1 bool, __a2 User) ui.Node { return Card(__a2, __a1) }(x, u).
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
			items = append(items, item{a, c.el.end, c.target.q + "Node"})
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
