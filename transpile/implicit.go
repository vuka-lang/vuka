package transpile

import (
	"go/ast"
	"go/types"
	"slices"
	"strings"
)

// A call in a template's markup whose function takes a context.Context
// first, and which is one argument short of it, takes the render's ctx
// there:
//
//	{GetCategories()}                  →   {GetCategories(ctx)}
//	for _, a := range Adverts(page) {  →   for _, a := range Adverts(ctx, page) {
//
// Only a call that doesn't compile as written is: GetCategories(ctx) is
// left as it is, and so is a call whose first parameter is anything else.
// Go code — a .vuka file, a template's header — passes its contexts
// itself, and a method value (for={GetCategories}) is no call. A call
// still short of its arguments is E0162: markup has no services to pass.

// ImplicitCtx is a call in markup given the render's ctx: Off its first
// byte and Lparen its (, in the file as written; Args when it has
// arguments of its own, which the ctx goes before.
type ImplicitCtx struct {
	Off, Lparen int
	Args        bool
}

// markupSpans are the spans of a template's markup holding Go a call in
// markup may be in: holes, attributes (not event handlers), blocks'
// headers and statements.
func (e *engine) markupSpans(f *fileState) []span {
	h := f.html
	if h == nil {
		return nil
	}
	var out []span
	var walk func(ns []jsxNode)
	walk = func(ns []jsxNode) {
		for _, n := range ns {
			switch n := n.(type) {
			case *jsxElem:
				for _, a := range n.attrs {
					switch {
					case a.event || a.stmts || a.bind != nil || a.write != nil || strings.HasPrefix(a.name, "bind:"):
					case a.kind == 'e' || a.kind == 'r':
						out = append(out, a.expr)
					case a.kind == 'p':
						for _, p := range a.parts {
							out = append(out, p.expr)
						}
					}
				}
				walk(n.kids)
			case *jsxHole:
				if n.bind == nil {
					out = append(out, n.expr)
				}
			case *jsxBlock:
				if n.m == nil {
					out = append(out, n.heads...)
				}
				for _, b := range n.bodies {
					walk(b)
				}
				for _, c := range n.cases {
					walk(c)
				}
			}
		}
	}
	for _, t := range f.jsx {
		if h.toSrc(t.start) >= h.markup {
			walk([]jsxNode{t.root})
		}
	}
	return out
}

// markupRoots are the Go of f's markup in this round's syntax tree: its
// holes' and attributes' expressions, its blocks' headers' and its
// statements'.
func (e *engine) markupRoots(f *fileState) []ast.Node {
	spans := e.markupSpans(f)
	var out []ast.Node
	var rest []span
	for _, s := range spans {
		if x := e.exprAt(f, s); x != nil {
			out = append(out, x)
		} else {
			rest = append(rest, s)
		}
	}
	if len(rest) == 0 {
		return out
	}
	inRest := func(at int) bool {
		for _, s := range rest {
			if at >= s.start && at < s.end {
				return true
			}
		}
		return false
	}
	// The blocks' headers and the statements, in place: not the body of a
	// function literal written in one.
	userLit := func(n ast.Node) bool {
		for p := f.parents[n]; p != nil; p = f.parents[p] {
			if _, ok := p.(*ast.FuncLit); ok && inRest(f.orig(p.Pos())) {
				return true
			}
		}
		return false
	}
	ast.Inspect(f.ast, func(n ast.Node) bool {
		st, ok := n.(ast.Stmt)
		if !ok || f.off(n.Pos()) >= f.body || !inRest(f.orig(st.Pos())) || userLit(st) {
			return true
		}
		var xs []ast.Node
		switch st := st.(type) {
		case *ast.IfStmt:
			xs = []ast.Node{st.Init, st.Cond}
		case *ast.ForStmt:
			xs = []ast.Node{st.Init, st.Cond, st.Post}
		case *ast.RangeStmt:
			xs = []ast.Node{st.X}
		case *ast.SwitchStmt:
			xs = []ast.Node{st.Init, st.Tag}
		case *ast.CaseClause:
			for _, x := range st.List {
				xs = append(xs, x)
			}
		case *ast.BlockStmt, *ast.LabeledStmt:
			return true
		default:
			xs = []ast.Node{st}
		}
		for _, x := range xs {
			if x != nil {
				out = append(out, x)
			}
		}
		return true
	})
	return out
}

// markupCalls are the calls in f's markup, in this round's syntax tree:
// not in a function literal, whose body is Go's.
func (e *engine) markupCalls(f *fileState) []*ast.CallExpr {
	seen := map[*ast.CallExpr]bool{}
	var out []*ast.CallExpr
	for _, r := range e.markupRoots(f) {
		ast.Inspect(r, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.CallExpr:
				if !seen[n] {
					seen[n] = true
					out = append(out, n)
				}
			}
			return true
		})
	}
	return out
}

// markupIdents are the identifiers f's markup names, function literals'
// bodies included: not a selector's field or method, not a composite
// literal's key.
func (e *engine) markupIdents(f *fileState) []*ast.Ident {
	seen := map[*ast.Ident]bool{}
	var out []*ast.Ident
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			ast.Inspect(n.X, visit)
			return false
		case *ast.KeyValueExpr:
			if _, ok := n.Key.(*ast.Ident); ok {
				ast.Inspect(n.Value, visit)
				return false
			}
		case *ast.Ident:
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
		return true
	}
	for _, r := range e.markupRoots(f) {
		ast.Inspect(r, visit)
	}
	return out
}

// implicitCtx gives the render's ctx to f's calls one argument short of
// it, and reports the calls short of anything else (E0162). It reports
// whether it changed the source. A markup whose function has no ctx yet
// gets one: the package compiles again (Package), its file with one.
func (e *engine) implicitCtx(f *fileState) bool {
	h := f.html
	if e.info == nil || h == nil || h.recvType == "" || !f.partsDone {
		return false
	}
	if f.ctxDone == nil {
		f.ctxDone = map[span]bool{}
	}
	changed := false
	for _, c := range e.markupCalls(f) {
		key := span{f.orig(c.Pos()), f.orig(c.End())}
		if f.ctxDone[key] {
			continue
		}
		sig := e.callSig(c)
		if sig == nil {
			continue
		}
		args, ok := e.args(c)
		if !ok {
			continue
		}
		switch e.ctxFits(sig, args, c.Ellipsis.IsValid()) {
		case fitsAsWritten:
			f.ctxDone[key] = true
		case fitsWithCtx:
			f.ctxDone[key] = true
			if h.ownCtx {
				continue // its ctx is the markup's own
			}
			if !h.contextual {
				if e.forceCtx == nil {
					e.forceCtx = map[string]bool{}
				}
				e.forceCtx[f.name], e.recompile = true, true
				return false
			}
			e.insertCtx(f, c)
			changed = true
		case short:
			f.ctxDone[key] = true
			e.shortCall(f, c, sig, len(args))
		}
	}
	if changed {
		e.progress = true
	}
	return changed
}

// insertCtx writes the ctx into c's arguments.
func (e *engine) insertCtx(f *fileState, c *ast.CallExpr) {
	lp := f.orig(c.Lparen)
	text := "ctx"
	if len(c.Args) > 0 {
		text = "ctx, "
	}
	f.insert(lp+1, text, -2)
	f.implicit = append(f.implicit, ImplicitCtx{Off: f.orig(c.Pos()), Lparen: lp, Args: len(c.Args) > 0})
}

// callSig is the signature a call calls: a function's, not a builtin's or
// a conversion's.
func (e *engine) callSig(c *ast.CallExpr) *types.Signature {
	tv, ok := e.info.Types[c.Fun]
	if !ok || tv.Type == nil || tv.IsType() || tv.IsBuiltin() {
		return nil
	}
	if sel, ok := ast.Unparen(c.Fun).(*ast.SelectorExpr); ok {
		if s := e.info.Selections[sel]; s != nil && s.Kind() == types.MethodExpr {
			return nil // a bare name markup may yet read as its struct's field
		}
	}
	sig, _ := types.Unalias(tv.Type).Underlying().(*types.Signature)
	if sig == nil || invalidSig(sig) {
		return nil
	}
	return sig
}

type ctxFit int

const (
	fitsOther     ctxFit = iota // nothing to say: wrong types, too many arguments
	fitsAsWritten               // compiles as written
	fitsWithCtx                 // compiles with the render's ctx first
	short                       // short of an argument markup can't pass
)

// ctxFits says how a call with args fits sig, with or without a ctx
// before them.
func (e *engine) ctxFits(sig *types.Signature, args []arg, spread bool) ctxFit {
	if _, ok := matchSig(sig, args, spread); ok {
		return fitsAsWritten
	}
	params := sig.Params()
	if params.Len() == 0 {
		return fitsOther
	}
	first := params.At(0).Type()
	if isContext(first) && !(sig.Variadic() && params.Len() == 1) {
		with := append([]arg{{t: first}}, args...)
		if _, ok := matchSig(sig, with, spread); ok {
			return fitsWithCtx
		}
		if sig.TypeParams().Len() > 0 && arity(sig, len(with), spread) {
			// A generic function's parameters fit once inferred: the
			// compiler says when they don't.
			return fitsWithCtx
		}
	}
	if e.shortOf(sig, args, spread) >= 0 {
		return short
	}
	return fitsOther
}

// arity reports whether n arguments are as many as sig takes.
func arity(sig *types.Signature, n int, spread bool) bool {
	np := sig.Params().Len()
	if sig.Variadic() && !spread {
		return n >= np-1
	}
	return n == np
}

// shortOf is the index of the first parameter a call with args doesn't
// reach, the arguments it has fitting those before it — after a ctx
// markup passes, when sig takes one first — or -1.
func (e *engine) shortOf(sig *types.Signature, args []arg, spread bool) int {
	params := sig.Params()
	n := params.Len()
	if sig.Variadic() {
		n-- // the variadic one needs nothing
	}
	from := 0
	if n > 0 && isContext(params.At(0).Type()) {
		from = 1
	}
	if spread || from+len(args) >= n {
		return -1
	}
	for i, a := range args {
		if _, ok := argScore(a, params.At(from+i).Type()); !ok && sig.TypeParams().Len() == 0 {
			return -1
		}
	}
	return from + len(args)
}

// shortCall reports a call in markup still short of an argument: E0162,
// naming the missing parameter's type.
func (e *engine) shortCall(f *fileState, c *ast.CallExpr, sig *types.Signature, nargs int) {
	args, _ := e.args(c)
	i := e.shortOf(sig, args, c.Ellipsis.IsValid())
	if i < 0 {
		return
	}
	p := sig.Params().At(i)
	name := calledName(c)
	at := f.at(f.orig(c.Pos()))
	t := e.display(p.Type())
	text := string(f.src[f.orig(c.Pos()):f.orig(c.End())])
	var err *Error
	if serviceLike(p.Type()) {
		ctor := "New" + f.html.recvType
		err = e.errs.add("E0162", at, "%s needs a %s: markup can't pass services; take it in %s", name, t, ctor).
			help("take the %s in %s, keep it in a field of %s, and pass the field", t, ctor, f.html.recvType)
	} else {
		err = e.errs.add("E0162", at, "%s needs a %s as its argument %d", name, t, i+1-ctxShift(sig)).
			help("pass it: markup passes only the render's ctx itself")
	}
	if !strings.Contains(text, "\n") {
		err.Len = len(text)
	}
	_ = nargs
}

func ctxShift(sig *types.Signature) int {
	if sig.Params().Len() > 0 && isContext(sig.Params().At(0).Type()) {
		return 1
	}
	return 0
}

// serviceLike reports whether t is what a page's constructor takes from
// the container: a pointer to a struct, or an interface but a context.
func serviceLike(t types.Type) bool {
	if isContext(t) || types.Identical(t, errorType) {
		return false
	}
	switch u := types.Unalias(t).Underlying().(type) {
	case *types.Pointer:
		_, ok := u.Elem().Underlying().(*types.Struct)
		return ok
	case *types.Interface:
		_, named := types.Unalias(t).(*types.Named)
		return named
	}
	return false
}

// calledName is the name a call calls, as messages give it.
func calledName(c *ast.CallExpr) string {
	switch fun := ast.Unparen(c.Fun).(type) {
	case *ast.Ident:
		return fun.Name
	case *ast.SelectorExpr:
		return fun.Sel.Name
	case *ast.IndexExpr:
		return calledName(&ast.CallExpr{Fun: fun.X})
	case *ast.IndexListExpr:
		return calledName(&ast.CallExpr{Fun: fun.X})
	}
	return types.ExprString(c.Fun)
}

// implicitOf are f's calls given the render's ctx, at their offsets in the
// file as written.
func (f *fileState) implicitOf() []ImplicitCtx {
	out := slices.Clone(f.implicit)
	if f.html != nil {
		for i := range out {
			c := &out[i]
			c.Off, c.Lparen = f.html.toSrc(c.Off), f.html.toSrc(c.Lparen)
		}
	}
	return out
}
