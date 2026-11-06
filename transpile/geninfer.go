package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
)

// A generic component's tag infers its type arguments from its attributes,
// as Go infers a call's: each attribute's value is unified with the type of
// the field or parameter it sets, which pins the type parameters that type
// mentions.
//
//	type DataTable[T any] struct{ Data TableData[T]; Row func(T) ui.Node }
//	<DataTable data={orders()}>          // orders() is a TableData[Order]: T = Order
//
// Type arguments written in the tag win; an attribute whose value would
// pin one differently, and which doesn't fit as written, is E0181.

// inferErr is why a tag's type arguments aren't known: E0181, or an E0106
// from binding its attributes.
type inferErr struct {
	code      diag.Code
	off       int
	msg, help string
	suggest   string
}

func bindErr(off int, msg string) *inferErr { return &inferErr{code: "E0106", off: off, msg: msg} }

func (e *engine) reportInfer(f *fileState, c *jsxComp, ie *inferErr) {
	c.dead = true
	err := e.errs.add(ie.code, f.at(ie.off), "%s", ie.msg)
	if ie.help != "" {
		err.help("%s", ie.help)
	}
	if ie.suggest != "" {
		err.suggest(ie.suggest)
	}
}

// instantiate infers a generic function component's type arguments. A
// function taking one generic props struct is unified field by field
// (inferProps); any other is inferred by go/types from the call its tag
// makes, where a parameter it leaves out takes nil and so infers nothing.
// It returns (nil, nil) while an attribute's type is unknown.
func (e *engine) instantiate(f *fileState, c *jsxComp, fun ast.Expr, sig *types.Signature, attrs []*jsxAttr, node types.Type) (*types.Signature, *inferErr) {
	el := c.el
	if st, _ := propsStruct(sig, attrs); st != nil {
		return e.inferProps(f, c, sig, st, attrs, node)
	}
	trial := &jsxComp{el: el, target: c.target}
	if off, msg := e.bindComp(trial, sig, attrs, node, true); msg != "" {
		return nil, bindErr(off, msg)
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
				return nil, nil
			}
		}
		call.Args = append(call.Args, x)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Instances: map[*ast.Ident]types.Instance{}}
	_ = types.CheckExpr(e.fset, e.pkg, pos, call, info) // a mismatch is reported where the call is made
	if inst, ok := info.Instances[compIdent(fun)]; ok {
		if s, ok := inst.Type.(*types.Signature); ok && s.TypeParams().Len() == 0 {
			return s, nil
		}
	}
	var names []string
	var typs []types.Type
	for i := 0; i < sig.Params().Len(); i++ {
		if p := sig.Params().At(i); !isContext(p.Type()) {
			names, typs = append(names, p.Name()), append(typs, p.Type())
		}
	}
	return e.solved(f, el, sig, sig.TypeParams(), trial.args, names, typs, node)
}

// inferProps infers the type arguments of a function component whose one
// parameter is a generic props struct, which go/types can't reach: the
// struct is written as a literal, not passed as arguments.
func (e *engine) inferProps(f *fileState, c *jsxComp, sig *types.Signature, st *types.Struct, attrs []*jsxAttr, node types.Type) (*types.Signature, *inferErr) {
	trial := &jsxComp{el: c.el, target: c.target}
	if off, msg := e.bindComp(trial, sig, attrs, node, true); msg != "" {
		return nil, bindErr(off, msg)
	}
	names, typs := fieldsOf(e.propFields(c.target, st, false))
	return e.solved(f, c.el, sig, sig.TypeParams(), trial.args, names, typs, node)
}

// solved instantiates sig with the type arguments its attributes pin.
func (e *engine) solved(f *fileState, el *jsxElem, sig *types.Signature, tps *types.TypeParamList, args []*compArg, names []string, typs []types.Type, node types.Type) (*types.Signature, *inferErr) {
	inst, ie := e.solve(f, el, sig, tps, args, names, typs, node)
	if inst == nil {
		return nil, ie
	}
	if s, ok := inst.(*types.Signature); ok {
		return s, nil
	}
	return nil, e.underErr(f, el, tps, nil, names, typs, node)
}

// inferStateful infers a generic struct component's type arguments, the
// text of which its literal carries (c.targText): Go can't infer a
// composite literal's.
func (e *engine) inferStateful(f *fileState, c *jsxComp, n *types.Named, attrs []*jsxAttr, node types.Type) (types.Type, *inferErr) {
	el := c.el
	st, ok := n.Underlying().(*types.Struct)
	if !ok {
		return nil, &inferErr{code: "E0181", off: el.start, msg: "<" + el.tag + "> is generic: write its type arguments, <" + el.tag + "[…]>"}
	}
	trial := &jsxComp{el: el, target: c.target}
	if _, off, msg := e.bindFields(trial, st, attrs, node, true); msg != "" {
		return nil, bindErr(off, msg)
	}
	names, typs := fieldsOf(e.propFields(c.target, st, true))
	inst, ie := e.solve(f, el, n, n.TypeParams(), trial.args, names, typs, node)
	if inst == nil {
		return nil, ie
	}
	targs := inst.(*types.Named).TypeArgs()
	texts := make([]string, targs.Len())
	for i := range texts {
		texts[i] = e.typeTextAuto(f, targs.At(i))
	}
	c.targText = "[" + strings.Join(texts, ", ") + "]"
	return inst, nil
}

func fieldsOf(fields []propField) (names []string, typs []types.Type) {
	for _, fl := range fields {
		names, typs = append(names, lowerFirst(fl.name)), append(typs, fl.typ)
	}
	return names, typs
}

// solve pins tps from args and instantiates generic with them; (nil, nil)
// while an attribute's type is unknown.
func (e *engine) solve(f *fileState, el *jsxElem, generic types.Type, tps *types.TypeParamList, args []*compArg, names []string, typs []types.Type, node types.Type) (types.Type, *inferErr) {
	targs, u, status := e.solveTargs(f, args, tps)
	switch status {
	case inferRetry:
		return nil, nil
	case inferConflict:
		return nil, e.conflictErr(f, el, tps, u)
	case inferUnder:
		return nil, e.underErr(f, el, tps, u.subst, names, typs, node)
	}
	inst, err := types.Instantiate(nil, generic, targs, true)
	if err != nil {
		return nil, &inferErr{code: "E0181", off: el.start, msg: "<" + el.tag + ">'s inferred type arguments don't fit its constraints (" + err.Error() + ")", help: "write them in the tag", suggest: e.explicitForm(f, el, tps, u.subst)}
	}
	return inst, nil
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

// attrConc is an attribute value's type for inference, untyped constants
// kept so: an untyped string for quoted text, an untyped bool for a bare
// attribute. ok is false while the expression's type isn't known yet.
func (e *engine) attrConc(f *fileState, a *jsxAttr) (conc types.Type, ok bool) {
	switch a.kind {
	case 's':
		return types.Typ[types.UntypedString], true
	case 'b':
		return types.Typ[types.UntypedBool], true
	default:
		x := e.attrExpr(f, a)
		tv, found := e.info.Types[x]
		if x == nil || !found || isInvalid(tv.Type) {
			return nil, false
		}
		if tv.Value != nil || tv.IsNil() {
			// a constant's recorded type is the one its place converted it
			// to; alone it may be untyped
			info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
			if types.CheckExpr(e.fset, e.pkg, x.Pos(), x, info) == nil {
				return info.Types[x].Type, true
			}
		}
		return tv.Type, true
	}
}

func untyped(t types.Type) (*types.Basic, bool) {
	b, ok := t.(*types.Basic)
	return b, ok && b.Info()&types.IsUntyped != 0
}

// solveTargs pins tps as Go does: first by unifying each typed attribute's
// value with the declared type it sets, then each parameter still unbound
// that some attributes set directly with untyped constants by the default
// type of the largest of them (1 and 2.5 make float64). Children, slots and
// left-out fields infer nothing, nor does nil.
func (e *engine) solveTargs(f *fileState, args []*compArg, tps *types.TypeParamList) ([]types.Type, *unifier, inferStatus) {
	u := &unifier{tps: tps, subst: map[*types.TypeParam]types.Type{}, from: map[*types.TypeParam]*jsxAttr{}}
	type loose struct {
		tp   *types.TypeParam
		attr *jsxAttr
		typ  *types.Basic
	}
	var consts []loose
	for _, a := range args {
		if a.attr == nil {
			continue
		}
		conc, ok := e.attrConc(f, a.attr)
		if !ok {
			return nil, u, inferRetry
		}
		if b, ok := untyped(conc); ok {
			if tp, ok := types.Unalias(a.typ).(*types.TypeParam); ok && u.has(tp) && b.Kind() != types.UntypedNil {
				consts = append(consts, loose{tp, a.attr, b})
			}
			continue
		}
		u.at = a.attr
		if !u.unify(a.typ, conc) {
			return nil, u, inferConflict
		}
	}
	picked := map[*types.TypeParam]loose{}
	for _, c := range consts {
		if t, bound := u.subst[c.tp]; bound {
			if !types.AssignableTo(c.typ, t) {
				u.bad, u.was, u.now, u.at = c.tp, t, types.Default(c.typ), c.attr
				return nil, u, inferConflict
			}
			continue
		}
		prev, ok := picked[c.tp]
		switch {
		case !ok:
			picked[c.tp] = c
		case prev.typ.Kind() == c.typ.Kind():
		case numericConst(prev.typ) && numericConst(c.typ):
			if c.typ.Kind() > prev.typ.Kind() {
				picked[c.tp] = c
			}
		default:
			u.bad, u.was, u.now, u.at = c.tp, types.Default(prev.typ), types.Default(c.typ), c.attr
			u.from[c.tp] = prev.attr
			return nil, u, inferConflict
		}
	}
	for tp, c := range picked {
		u.subst[tp], u.from[tp] = types.Default(c.typ), c.attr
	}
	targs := make([]types.Type, tps.Len())
	status := inferOK
	for i := range targs {
		t, ok := u.subst[tps.At(i)]
		if !ok {
			status = inferUnder
		}
		targs[i] = t
	}
	return targs, u, status
}

// numericConst reports whether an untyped constant is a number: its kinds
// are ordered int < rune < float < complex, as Go's inference picks them.
func numericConst(b *types.Basic) bool { return b.Info()&types.IsNumeric != 0 }

// explicitForm is the tag with its type arguments written: those subst
// pins, and each other parameter by its name.
func (e *engine) explicitForm(f *fileState, el *jsxElem, tps *types.TypeParamList, subst map[*types.TypeParam]types.Type) string {
	args := make([]string, tps.Len())
	for i := range args {
		if t, ok := subst[tps.At(i)]; ok {
			args[i] = e.typeTextAuto(f, t)
		} else {
			args[i] = tps.At(i).Obj().Name()
		}
	}
	return "<" + baseTag(el.tag) + "[" + strings.Join(args, ", ") + "]>"
}

// baseTag is a tag without the type arguments it writes: List of List[T].
func baseTag(tag string) string {
	if i := strings.IndexByte(tag, '['); i >= 0 {
		return tag[:i]
	}
	return tag
}

// underErr says which type parameters no attribute pins, the explicit form,
// and the first prop (not a slot) whose type would pin the first of them.
func (e *engine) underErr(f *fileState, el *jsxElem, tps *types.TypeParamList, subst map[*types.TypeParam]types.Type, names []string, typs []types.Type, node types.Type) *inferErr {
	var missing []string
	var first *types.TypeParam
	for i := 0; i < tps.Len(); i++ {
		if _, ok := subst[tps.At(i)]; !ok {
			if first == nil {
				first = tps.At(i)
			}
			missing = append(missing, tps.At(i).Obj().Name())
		}
	}
	what := "type parameter "
	if len(missing) > 1 {
		what = "type parameters "
	}
	ie := &inferErr{code: "E0181", off: el.start,
		msg:  "<" + el.tag + "> is generic, and its attributes don't give its " + what + strings.Join(missing, ", "),
		help: "write the type arguments in the tag: <" + baseTag(el.tag) + "[…]>"}
	if len(missing) < tps.Len() {
		ie.help, ie.suggest = "write the type arguments in the tag", e.explicitForm(f, el, tps, subst)
	}
	for i, t := range typs {
		if first != nil && e.slotKind(t, node) != slotFunc && mentions(t, first) {
			ie.help = "set " + names[i] + "={…}, whose type " + e.display(t) + " gives " + first.Obj().Name() + ", or write the type arguments in the tag"
			break
		}
	}
	return ie
}

// conflictErr is an attribute pinning a type parameter another one pinned
// to a different type, reported at the attribute.
func (e *engine) conflictErr(f *fileState, el *jsxElem, tps *types.TypeParamList, u *unifier) *inferErr {
	tp := u.bad.Obj().Name()
	off, msg := el.start, "<"+el.tag+"> is generic, and its attributes make "+tp+" both "+e.display(u.was)+" and "+e.display(u.now)
	if u.at != nil {
		off = u.at.off
		msg = "<" + el.tag + ">'s " + u.at.name + " makes " + tp + " " + e.display(u.now)
		if prev := u.from[u.bad]; prev != nil {
			msg += ", but its " + prev.name + " made it " + e.display(u.was)
		} else {
			msg += ", but it is " + e.display(u.was)
		}
	}
	subst := map[*types.TypeParam]types.Type{}
	for k, v := range u.subst {
		subst[k] = v
	}
	subst[u.bad] = u.was
	return &inferErr{code: "E0181", off: off, msg: msg, help: "give them one type, or write the type arguments in the tag", suggest: e.explicitForm(f, el, tps, subst)}
}

// explicitErr reports the first attribute of a tag whose type arguments are
// written that doesn't fit as written and would pin one of them to another
// type. generic are its attributes bound to the generic declaration,
// bound to the instance.
func (e *engine) explicitErr(f *fileState, el *jsxElem, tps *types.TypeParamList, targs []types.Type, generic, bound []*compArg) *inferErr {
	if tps.Len() != len(targs) {
		return nil
	}
	decl := map[*jsxAttr]types.Type{}
	eachArg(generic, func(a *compArg) {
		if a.attr != nil {
			decl[a.attr] = a.typ
		}
	})
	var found *inferErr
	eachArg(bound, func(a *compArg) {
		pat := decl[a.attr]
		if found != nil || a.attr == nil || pat == nil || a.typ == nil {
			return
		}
		conc, ok := e.attrConc(f, a.attr)
		if _, loose := untyped(conc); !ok || loose || types.AssignableTo(conc, a.typ) {
			return
		}
		u := &unifier{tps: tps, subst: map[*types.TypeParam]types.Type{}, from: map[*types.TypeParam]*jsxAttr{}}
		u.unify(pat, conc)
		for i := 0; i < tps.Len(); i++ {
			got, ok := u.subst[tps.At(i)]
			if !ok || types.Identical(got, targs[i]) {
				continue
			}
			name := tps.At(i).Obj().Name()
			found = &inferErr{code: "E0181", off: a.attr.off,
				msg:  "<" + el.tag + ">'s " + a.attr.name + " is " + e.display(conc) + ", which makes " + name + " " + e.display(got) + ", but the tag writes " + name + " as " + e.display(targs[i]),
				help: "pass a value of type " + e.display(a.typ) + ", or drop the tag's type arguments to infer them: <" + baseTag(el.tag) + ">"}
			return
		}
	})
	return found
}

func typeList(l *types.TypeList) []types.Type {
	out := make([]types.Type, l.Len())
	for i := range out {
		out[i] = l.At(i)
	}
	return out
}

// mentions reports whether t refers to the type parameter tp.
func mentions(t types.Type, tp *types.TypeParam) bool {
	seen := map[types.Type]bool{}
	var walk func(types.Type) bool
	walk = func(t types.Type) bool {
		t = types.Unalias(t)
		if t == nil || seen[t] {
			return false
		}
		seen[t] = true
		switch t := t.(type) {
		case *types.TypeParam:
			return t == tp
		case *types.Pointer:
			return walk(t.Elem())
		case *types.Slice:
			return walk(t.Elem())
		case *types.Array:
			return walk(t.Elem())
		case *types.Chan:
			return walk(t.Elem())
		case *types.Map:
			return walk(t.Key()) || walk(t.Elem())
		case *types.Signature:
			for _, tu := range []*types.Tuple{t.Params(), t.Results()} {
				for i := 0; i < tu.Len(); i++ {
					if walk(tu.At(i).Type()) {
						return true
					}
				}
			}
		case *types.Struct:
			for i := 0; i < t.NumFields(); i++ {
				if walk(t.Field(i).Type()) {
					return true
				}
			}
		case *types.Named:
			for i := 0; i < t.TypeArgs().Len(); i++ {
				if walk(t.TypeArgs().At(i)) {
					return true
				}
			}
		}
		return false
	}
	return walk(t)
}

// unifier accumulates type-parameter bindings while matching declared types
// against attribute values' types. go/types has no exported unifier, so this
// one covers the shapes props use: the parameter itself, pointers, slices,
// arrays, maps, channels, funcs and generic named types, nested.
type unifier struct {
	tps      *types.TypeParamList
	subst    map[*types.TypeParam]types.Type
	from     map[*types.TypeParam]*jsxAttr // the attribute that bound each
	at       *jsxAttr                      // the attribute being unified
	bad      *types.TypeParam              // the first parameter bound two incompatible ways
	was, now types.Type
}

// unify matches a declared type pat against a value's type conc, recording
// each parameter's binding. A shape the two don't share contributes nothing,
// so it returns false only on a parameter already bound to another type.
func (u *unifier) unify(pat, conc types.Type) bool {
	if tp, ok := types.Unalias(pat).(*types.TypeParam); ok && u.has(tp) {
		switch prev, ok := u.subst[tp]; {
		case !ok:
			u.subst[tp], u.from[tp] = conc, u.at
		case !types.Identical(prev, conc):
			u.bad, u.was, u.now = tp, prev, conc
			return false
		}
		return true
	}
	// A generic named type N[X,…] (TableData[T]) against an instance of
	// it: unify the arguments.
	if pn, ok := types.Unalias(pat).(*types.Named); ok && pn.TypeArgs().Len() > 0 {
		if cn, ok := types.Unalias(conc).(*types.Named); ok && cn.TypeArgs().Len() == pn.TypeArgs().Len() && types.Identical(pn.Origin(), cn.Origin()) {
			for i := 0; i < pn.TypeArgs().Len(); i++ {
				if !u.unify(pn.TypeArgs().At(i), cn.TypeArgs().At(i)) {
					return false
				}
			}
			return true
		}
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
	return true
}

func (u *unifier) unifyTuple(p, c *types.Tuple) bool {
	if p.Len() != c.Len() {
		return true
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
