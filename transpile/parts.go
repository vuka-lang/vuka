package transpile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"maps"
	"strconv"
	"strings"
)

// Parts of stateless pages. A method of a page or component marked with a
// timing attribute — @ui.Defer, @ui.Defer("group") — runs after the page's
// first bytes are sent, and the markup that calls it is its part: each
// element whose own markup (its attributes, text, blocks' headers — not a
// child element's) calls it renders once the method returns, its
// <ui.Fallback for={M}> until then.
//
//	@ui.Defer
//	func (p *Index) Stats(ctx context.Context) (Stats, error) { … }
//
//	<p>{Stats(ctx).Open} open</p>
//	<ui.Fallback for={Stats}><span class="skeleton"/></ui.Fallback>
//
// is
//
//	ui.Part(p, (*Index).Stats, ui.HTML(`<span class="skeleton"/>`), func(stats Stats) ui.Node {
//		return ui.HTML(`<p>{stats.Open} open</p>`, stats.Open)
//	})
//
// The calls become the parameter; one element calling two deferred methods
// is one part inside the other's. The method may be called from the markup
// only, and only where an element takes its result: see partErrors.

// deferMethod is a method of a template's struct its timing attribute
// defers.
type deferMethod struct {
	name     string
	obj      *types.Func
	result   types.Type // its T
	group    string
	attr     *Attr
	attrFile *fileState
	param    string // the name its value has in the part's function
	typ      string // its T, as the file spells it
	expr     string // its method expression: (*Index).Stats, Index.Stats
	recv     string // the value the markup calls it on: p, or *p for a value receiver's
	fallback *jsxElem
	failed   *jsxElem // its <ui.Fallback for={M} error>
	parts    int
	called   bool // the markup calls it, a part or not
}

// partUse is a deferred method an element's part waits for: its
// fallbacks a binding's own, when a <ui.Fallback> names the binding.
type partUse struct {
	m                *deferMethod
	binding          bool
	fallback, failed *jsxElem
}

// timingAttrs are the attributes saying when a method of a page or
// component runs; Defer is phase 1's, the others phase 2's (none yet).
var timingAttrs = map[string]bool{"Defer": true}

// timingAttr reports whether a, written in f, is a timing attribute: one
// of timingAttrs, a type of a package rendering parts (declaring Part, as
// ui does). It returns its name (Defer) when it is.
func (e *engine) timingAttr(f *fileState, a *Attr) string {
	if a.Field || a.Param || a.kind == attrHook {
		return ""
	}
	pkgName, sel, ok := strings.Cut(a.Name, ".")
	if !ok || !timingAttrs[sel] {
		return ""
	}
	pkg := e.importedAs(f, pkgName)
	if pkg == nil {
		return ""
	}
	_, typ := pkg.Scope().Lookup(sel).(*types.TypeName)
	_, part := pkg.Scope().Lookup("Part").(*types.Func)
	if !typ || !part {
		return ""
	}
	return sel
}

// findParts decides the parts of f's markup, once its types are known: the
// deferred methods of its struct, the elements calling them, their
// fallbacks; the calls become the part functions' parameters. It reports
// whether it changed the source.
func (e *engine) findParts(f *fileState) bool {
	h := f.html
	if f.partsDone || e.info == nil || h == nil || h.recvType == "" || h.shape == nil || f.target == nil || e.pkg == nil {
		return false
	}
	obj, _ := e.pkg.Scope().Lookup(h.recvType).(*types.TypeName)
	if obj == nil {
		return false
	}
	if !f.susChecked {
		f.susChecked = true
		e.checkSuspense(f)
	}
	methods := e.deferredOf(h.recvType)
	if len(methods) == 0 {
		f.partsDone = true
		e.strayFallbacks(f)
		return false
	}
	ptr := types.NewPointer(obj.Type())
	for _, m := range methods {
		// Decided once the methods' types are: an imported package's may
		// be known only in a later round.
		if o, _, _ := types.LookupFieldOrMethod(ptr, true, e.pkg, m.name); o != nil && invalidSig(o.Type().(*types.Signature)) {
			return false
		}
	}
	f.partsDone = true
	nerrs := len(*e.errs)
	byObj := map[*types.Func]*deferMethod{}
	var names []string
	for _, m := range methods {
		o, _, _ := types.LookupFieldOrMethod(ptr, true, e.pkg, m.name)
		fn, ok := o.(*types.Func)
		if !ok {
			continue
		}
		m.obj = fn
		names = append(names, m.name)
		at := m.attr.Pos
		sig := fn.Type().(*types.Signature)
		res := sig.Results()
		switch {
		case h.shape.Live:
			e.errs.add("E0346", at, "@%s is for stateless pages; %s is live (%s): a live page loads in its @ui.Mount hook and pushes with Update", m.attr.Name, h.recvType, h.shape.LiveWhy).
				help("drop the attribute and load %s in @ui.Mount into a field the markup reads", m.name)
		case sig.Params().Len() < 1 || !isContext(sig.Params().At(0).Type()) || res.Len() != 2 || !types.Identical(res.At(1).Type(), errorType):
			e.errs.add("E0347", at, "@%s: %s.%s is %s; a deferred method takes a context.Context and returns (T, error)", m.attr.Name, h.recvType, m.name, e.display(sig)).
				help("func (%s *%s) %s(ctx context.Context) (T, error)", or(h.recv, "p"), h.recvType, m.name)
		case !f.target.has("Part"):
			e.errs.add("E0347", at, "@%s: %s has no Part to render %s's part with", m.attr.Name, f.target.path, m.name).help("update %s", f.target.path)
		default:
			m.result = res.At(0).Type()
			byObj[fn] = m
		}
	}
	if len(*e.errs) > nerrs {
		return false
	}
	// The calls in the markup, by the element whose part each is.
	type call struct {
		m          *deferMethod
		start, end int // the call
		ident      int // its method name's offset
		binding    bool
	}
	var calls []call
	allowed := map[int]bool{} // offsets of the method names the markup may name
	methodOf := func(c *ast.CallExpr) *deferMethod {
		switch fun := ast.Unparen(c.Fun).(type) {
		case *ast.Ident:
			if o := e.info.Uses[fun]; o != nil {
				fn, _ := o.(*types.Func)
				return byObj[fn]
			}
			o, _, _ := types.LookupFieldOrMethod(ptr, true, e.pkg, fun.Name)
			fn, _ := o.(*types.Func)
			return byObj[fn]
		case *ast.SelectorExpr:
			if s := e.info.Selections[fun]; s != nil {
				fn, _ := s.Obj().(*types.Func)
				return byObj[fn]
			}
		}
		return nil
	}
	nameOff := func(c *ast.CallExpr) int {
		switch fun := ast.Unparen(c.Fun).(type) {
		case *ast.SelectorExpr:
			return f.orig(fun.Sel.Pos())
		}
		return f.orig(c.Fun.Pos())
	}
	// in collects the deferred calls in the expression at s.
	in := func(x ast.Node, each func(*deferMethod, *ast.CallExpr)) {
		if x == nil {
			return
		}
		ast.Inspect(x, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			if c, ok := n.(*ast.CallExpr); ok {
				if m := methodOf(c); m != nil {
					each(m, c)
				}
			}
			return true
		})
	}
	headCalls := func(hd span, each func(*deferMethod, *ast.CallExpr)) {
		ast.Inspect(f.ast, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			at := f.orig(c.Pos())
			if at >= hd.start && at < hd.end {
				if m := methodOf(c); m != nil {
					each(m, c)
				}
			}
			return true
		})
	}
	var fallbacks []*jsxElem
	var bound []*partBinding
	boundary := 0 // the <ui.Suspense>s walk is in
	var walk func(ns []jsxNode, el *jsxElem)
	walk = func(ns []jsxNode, el *jsxElem) {
		use := func(m *deferMethod, c *ast.CallExpr) {
			m.called = true
			if boundary > 0 {
				// Inside a <ui.Suspense>, which is its part: a call.
				allowed[nameOff(c)] = true
				return
			}
			if el == nil || el.frag {
				e.errs.add("E0350", f.at(f.orig(c.Pos())), "@%s: %s's call in %s's markup is in no element, which would be its part", m.attr.Name, m.name, h.recvType).
					help("put it inside an element: <span>{%s()}</span>", m.name)
				return
			}
			if !el.hasPart(m) {
				el.parts = append(el.parts, &partUse{m: m})
			}
			calls = append(calls, call{m: m, start: f.orig(c.Pos()), end: f.orig(c.End()), ident: nameOff(c)})
			allowed[nameOff(c)] = true
		}
		for _, n := range ns {
			switch n := n.(type) {
			case *jsxElem:
				if n.isFallbackFor(f) {
					fallbacks = append(fallbacks, n)
					continue
				}
				for _, a := range n.attrs {
					var xs []span
					switch a.kind {
					case 'e', 'r':
						xs = append(xs, a.expr)
					case 'p':
						for _, p := range a.parts {
							if p.expr.end > p.expr.start {
								xs = append(xs, p.expr)
							}
						}
					}
					for _, s := range xs {
						in(e.exprAt(f, s), func(m *deferMethod, c *ast.CallExpr) {
							m.called = true
							if n.comp != nil {
								e.errs.add("E0344", f.at(f.orig(c.Pos())), "@%s: %s's result goes to <%s %s=…>; defer it in %s instead", m.attr.Name, m.name, n.tag, a.name, lastName(n.tag)).
									help("give %s a deferred method of its own, or drop @%s", lastName(n.tag), m.attr.Name)
								return
							}
							use(m, c)
						})
					}
				}
				if n.isSuspense(f) {
					savedBoundary := boundary
					if attrNamed(n, "for") != nil {
						boundary = -1
					}
					boundary++
					walk(n.kids, n)
					boundary = savedBoundary
					continue
				}
				walk(n.kids, n)
			case *jsxHole:
				in(e.exprAt(f, n.expr), use)
			case *jsxBlock:
				if n.stmt {
					if f.susBindings[n.heads[0].start] != nil {
						if b := e.deferBinding(f, n.heads[0], methodOf); b != nil {
							b.el = el
							b.listed = true
							bound = append(bound, b)
						}
						headCalls(n.heads[0], func(m *deferMethod, c *ast.CallExpr) { m.called = true; allowed[nameOff(c)] = true })
						continue
					}
					if b := e.deferBinding(f, n.heads[0], methodOf); b != nil && boundary > 0 {
						b.m.called = true
						allowed[nameOff(b.call)] = true
						continue
					} else if b != nil {
						b.el = el
						bound = append(bound, b)
						b.m.called = true
						allowed[nameOff(b.call)] = true
						continue
					}
					if boundary > 0 {
						headCalls(n.heads[0], func(m *deferMethod, c *ast.CallExpr) {
							m.called, allowed[nameOff(c)] = true, true
						})
						continue
					}
					headCalls(n.heads[0], func(m *deferMethod, c *ast.CallExpr) {
						m.called = true
						e.errs.add("E0343", f.at(f.orig(c.Pos())), "@%s: %s is called in a template's statement, Go that runs as the page renders; call it in an element's markup", m.attr.Name, m.name).
							help("show it in a hole, <span>{%s()}</span>, or drop @%s", m.name, m.attr.Name)
					})
					continue
				}
				if n.m == nil {
					for _, hd := range n.heads {
						headCalls(hd, use)
					}
				}
				for _, b := range n.bodies {
					walk(b, el)
				}
				for _, c := range n.cases {
					walk(c, el)
				}
			}
		}
	}
	for _, t := range f.jsx {
		if h.toSrc(t.start) >= h.markup {
			walk([]jsxNode{t.root}, nil)
		}
	}
	// A deferred method's bindings: each one's element is its part, holding
	// every use of the name.
	for _, b := range bound {
		if b.listed || e.bindPart(f, b) {
			if f.bindDone == nil {
				f.bindDone = map[int]bool{}
			}
			f.bindDone[b.stmt.start] = true
			f.lazies = append(f.lazies, f.orig(b.id.Pos()))
			for _, u := range e.usesOf(f, e.info.Defs[b.id], b.id.Name) {
				end := f.orig(u.End())
				f.add(end-1, end, string(f.src[end-1:end])+"()")
			}
			calls = append(calls, call{m: b.m, start: f.orig(b.call.Pos()), end: f.orig(b.call.End()), ident: nameOff(b.call), binding: true})
		}
	}
	// An element inside another's part of the same method has its value
	// already: only the outermost calling it is a part.
	var outer func(ns []jsxNode, in map[*deferMethod]bool)
	outer = func(ns []jsxNode, in map[*deferMethod]bool) {
		for _, n := range ns {
			switch n := n.(type) {
			case *jsxElem:
				inner := in
				if len(n.parts) > 0 {
					kept := n.parts[:0]
					inner = maps.Clone(in)
					for _, p := range n.parts {
						if p.binding || !in[p.m] {
							kept = append(kept, p)
							if !p.binding {
								inner[p.m] = true
							}
							p.m.parts++
						}
					}
					n.parts = kept
				}
				outer(n.kids, inner)
			case *jsxBlock:
				for _, b := range n.bodies {
					outer(b, in)
				}
				for _, c := range n.cases {
					outer(c, in)
				}
			}
		}
	}
	for _, t := range f.jsx {
		if h.toSrc(t.start) >= h.markup {
			outer([]jsxNode{t.root}, map[*deferMethod]bool{})
		}
	}
	// The fallbacks: <ui.Fallback for={M}> and <ui.Fallback for={M} error>.
	for _, fb := range fallbacks {
		var forAttr *jsxAttr
		isErr := false
		for _, a := range fb.attrs {
			switch {
			case a.name == "for" && a.kind == 'e':
				forAttr = a
			case a.name == "error" && a.kind == 'b':
				isErr = true
			default:
				e.errs.add("E0348", f.at(a.off), "<%s> takes for={Method} and error, not %s", fb.tag, a.name).help("write <%s for={%s}>…</%s>", fb.tag, or(firstName(names), "Stats"), fb.tag)
			}
		}
		if forAttr == nil {
			e.errs.add("E0348", f.at(fb.start), "<%s> needs for={Method}: the deferred method whose parts it stands in for", fb.tag).help("write <%s for={%s}>…</%s>", fb.tag, or(firstName(names), "Stats"), fb.tag)
			continue
		}
		text := strings.TrimSpace(string(f.src[forAttr.expr.start:forAttr.expr.end]))
		text = strings.TrimPrefix(text, h.recv+".")
		if b := bindingNamed(bound, text, fb); b != nil {
			// for={categories}: the binding's own part.
			slot := &b.use.fallback
			if isErr {
				slot = &b.use.failed
			}
			if *slot != nil {
				e.errs.add("E0348", f.at(fb.start), "%s has a second <%s for={%s}%s>", h.recvType, fb.tag, text, map[bool]string{true: " error"}[isErr]).help("keep one: it stands in for %s's part", text)
				continue
			}
			*slot, fb.partFB = fb, b.m
			if fb.comp != nil {
				fb.comp.consumed, fb.comp.resolved = true, true
			}
			continue
		}
		var m *deferMethod
		for _, d := range methods {
			if d.name == text && d.obj != nil {
				m = d
			}
		}
		if m == nil {
			err := e.errs.add("E0348", f.at(forAttr.expr.start), "<%s for={%s}>: %s has no deferred method %s", fb.tag, text, h.recvType, text)
			if s := closestName(text, names); s != "" {
				err.Suggest = s
			} else if len(names) > 0 {
				err.help("name one of %s's deferred methods: %s", h.recvType, strings.Join(names, ", "))
			} else {
				err.help("mark the method @ui.Defer, or drop the fallback")
			}
			continue
		}
		in(e.exprAt(f, forAttr.expr), func(d *deferMethod, c *ast.CallExpr) {
			e.errs.add("E0348", f.at(f.orig(c.Pos())), "<%s for={…}> names the method, not a call: for={%s}", fb.tag, d.name).help("write for={%s}", d.name)
		})
		ast.Inspect(e.exprAt(f, forAttr.expr), func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == m.name {
				allowed[f.orig(id.Pos())] = true
			}
			return true
		})
		e.fallbackCalls(f, fb, byObj)
		slot := &m.fallback
		if isErr {
			slot = &m.failed
		}
		if *slot != nil {
			e.errs.add("E0348", f.at(fb.start), "%s has a second <%s for={%s}%s>", h.recvType, fb.tag, m.name, map[bool]string{true: " error"}[isErr]).help("keep one: it stands in for each of %s's parts", m.name)
			continue
		}
		*slot = fb
		fb.partFB = m
		if fb.comp != nil {
			fb.comp.consumed, fb.comp.resolved = true, true
		}
	}
	for _, m := range methods {
		if m.obj != nil && !m.called && byObj[m.obj] != nil {
			e.errs.add("E0342", m.attr.Pos, "@%s: %s's markup never calls %s, so there is no part to defer", m.attr.Name, h.recvType, m.name).
				help("show it in the markup, <span>{%s()}</span>, or drop @%s", m.name, m.attr.Name)
		}
	}
	e.goCalls(byObj, allowed)
	if len(*e.errs) > nerrs {
		return false
	}
	// Each method's value in its parts: its name, unless the markup names
	// something so.
	for _, m := range methods {
		if m.obj != nil {
			m.param = e.partParam(f, m)
			m.typ = e.typeTextAuto(f, m.result)
			m.expr, m.recv = h.recvType+h.shape.TypeArgs+"."+m.name, h.recv
			if _, ptr := m.obj.Type().(*types.Signature).Recv().Type().(*types.Pointer); ptr {
				m.expr = "(*" + h.recvType + h.shape.TypeArgs + ")." + m.name
			} else if h.shape.Ptr {
				m.recv = "*" + h.recv
			}
		}
	}
	for _, c := range calls {
		if c.binding {
			raw := string(f.src[c.start:c.end])
			lp := strings.IndexByte(raw, '(')
			if lp >= 0 {
				if !strings.Contains(raw[:lp], ".") {
					f.insert(c.start, h.recv+".", -2)
				}
				if expr, err := parser.ParseExpr(raw); err == nil {
					if call, ok := expr.(*ast.CallExpr); ok && len(call.Args)+1 == c.m.obj.Type().(*types.Signature).Params().Len() {
						arg := "ctx"
						if len(call.Args) > 0 {
							arg += ", "
						}
						f.insert(c.start+lp+1, arg, -2)
						f.implicit = append(f.implicit, ImplicitCtx{Off: c.start, Lparen: c.start + lp, Args: len(call.Args) > 0})
					}
				}
			}
			q := f.target.q
			f.insert(c.start, f.syncName()+".OnceValue(func() "+c.m.typ+" { return "+q+"LazyValue(ctx, func(ctx context.Context) ("+c.m.typ+", error) { return ", -3)
			f.add(c.end-1, c.end, string(f.src[c.end-1:c.end])+" }) })")
			if f.unwrapped == nil {
				f.unwrapped = map[span]bool{}
			}
			f.unwrapped[span{c.start, c.end}] = true
			f.done[c.ident] = true
			continue
		}
		kept := f.fixed[:0]
		for _, ed := range f.fixed {
			if ed.start >= c.start && ed.end <= c.end && ed.own == nil {
				continue // the call's rewrites: resolveBare's qualifier, the runtime's
			}
			kept = append(kept, ed)
		}
		f.fixed = kept
		f.done[c.ident] = true
		if f.unwrapped == nil {
			f.unwrapped = map[span]bool{}
		}
		f.unwrapped[span{c.start, c.end}] = true
		f.add(c.start, c.end, c.m.param)
	}
	e.progress = true
	return true
}

// hasPart reports whether el's part waits for m.
func (el *jsxElem) hasPart(m *deferMethod) bool {
	for _, p := range el.parts {
		if p.m == m {
			return true
		}
	}
	return false
}

// isFallbackFor reports whether el is a <ui.Fallback> tag of f's target.
func (el *jsxElem) isFallbackFor(f *fileState) bool {
	return el.comp != nil && f.target != nil && el.tag == f.target.q+"Fallback"
}

// fallbackCalls reports a fallback's own calls of a deferred method: it
// shows while they run.
func (e *engine) fallbackCalls(f *fileState, fb *jsxElem, byObj map[*types.Func]*deferMethod) {
	var walk func(ns []jsxNode)
	walk = func(ns []jsxNode) {
		for _, n := range ns {
			var x ast.Expr
			switch n := n.(type) {
			case *jsxHole:
				x = e.exprAt(f, n.expr)
			case *jsxElem:
				for _, a := range n.attrs {
					if a.kind == 'e' {
						e.fallbackCall(f, fb, e.exprAt(f, a.expr), byObj)
					}
				}
				walk(n.kids)
			case *jsxBlock:
				for _, b := range n.bodies {
					walk(b)
				}
			}
			e.fallbackCall(f, fb, x, byObj)
		}
	}
	walk(fb.kids)
}

func (e *engine) fallbackCall(f *fileState, fb *jsxElem, x ast.Expr, byObj map[*types.Func]*deferMethod) {
	if x == nil {
		return
	}
	ast.Inspect(x, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var fn *types.Func
		switch fun := ast.Unparen(c.Fun).(type) {
		case *ast.Ident:
			if o := e.info.Uses[fun]; o != nil {
				fn, _ = o.(*types.Func)
			} else if obj, ok := e.pkg.Scope().Lookup(f.html.recvType).(*types.TypeName); ok {
				o, _, _ := types.LookupFieldOrMethod(types.NewPointer(obj.Type()), true, e.pkg, fun.Name)
				fn, _ = o.(*types.Func)
			}
		case *ast.SelectorExpr:
			if s := e.info.Selections[fun]; s != nil {
				fn, _ = s.Obj().(*types.Func)
			}
		}
		if m := byObj[fn]; m != nil {
			e.errs.add("E0348", f.at(f.orig(c.Pos())), "<%s> calls %s, which it stands in for while %s runs", fb.tag, m.name, m.name).help("show what needs no deferred method: a skeleton, a spinner, text")
		}
		return true
	})
}

// goCalls reports the deferred methods named from Go: anywhere but the
// markup calls their parts take (allowed) — another method, a hook, a
// handler — where the result is needed now.
func (e *engine) goCalls(byObj map[*types.Func]*deferMethod, allowed map[int]bool) {
	for _, g := range e.files {
		for id, o := range e.info.Uses {
			fn, ok := o.(*types.Func)
			if !ok || byObj[fn] == nil || e.fileOf(id.Pos()) != g {
				continue
			}
			if g.off(id.Pos()) >= g.body {
				continue // the generated trailer: its registration
			}
			off := g.orig(id.Pos())
			if g.html != nil && allowed[off] {
				continue
			}
			m := byObj[fn]
			at := g.at(off)
			e.errs.add("E0343", at, "@%s: %s is called from Go (%s:%d), where its result is needed now; call it from the markup only", m.attr.Name, m.name, baseName(at.Filename), at.Line).
				help("drop @%s to call it here, or move what reads it into the markup", m.attr.Name)
		}
	}
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func firstName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

// partParam is the name m's value goes by in its parts' functions: the
// method's name, first letter lowered, unless the file names something so.
func (e *engine) partParam(f *fileState, m *deferMethod) string {
	base := lowerFirst(m.name)
	if token.IsKeyword(base) || types.Universe.Lookup(base) != nil {
		base += "V"
	}
	taken := map[string]bool{"ctx": true, f.html.recv: true}
	for _, imp := range f.ast.Imports {
		name := ImportName(strings.Trim(imp.Path.Value, `"`))
		if imp.Name != nil {
			name = imp.Name.Name
		}
		taken[name] = true
	}
	for n := range goIdents(markupGo(f.html.src[f.html.markup:])) {
		taken[n] = true
	}
	name := base
	for i := 2; taken[name]; i++ {
		name = base + strconv.Itoa(i)
	}
	return name
}

// deferredOf are the methods of the type named typ the package's timing
// attributes mark, in source order.
func (e *engine) deferredOf(typ string) []*deferMethod {
	var out []*deferMethod
	for _, f := range e.vuka {
		for _, a := range f.attrs {
			fd, ok := a.decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || recvBase(fd) != typ || e.timingAttr(f, a) == "" {
				continue
			}
			m := &deferMethod{name: fd.Name.Name, attr: a, attrFile: f}
			if v, ok := stringArg(strings.TrimSpace(a.Args)); ok {
				m.group = v
			}
			dup := false
			for _, o := range out {
				if o.name == m.name {
					e.errs.add("E0345", a.Pos, "%s has @%s and @%s: one says when it runs", m.name, o.attr.Name+o.attr.Args, a.Name+a.Args).
						help("keep one")
					dup = true
				}
			}
			if !dup {
				out = append(out, m)
			}
		}
	}
	return out
}

// strayTimings reports timing attributes on what isn't a method of a page
// or component: a function, or a method of a type no template renders.
func (e *engine) strayTimings() {
	rendered := map[string]bool{}
	for _, f := range e.vuka {
		if f.html != nil && f.html.recvType != "" && f.html.shape != nil {
			rendered[f.html.recvType] = true
		}
	}
	for _, f := range e.vuka {
		for _, a := range f.attrs {
			if e.timingAttr(f, a) == "" {
				continue
			}
			fd, ok := a.decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			if fd.Recv == nil {
				e.errs.add("E0341", a.Pos, "@%s goes on a page's or component's method: %s is a function", a.Name, fd.Name.Name).
					help("make it a method of the page whose markup shows it, or drop @%s", a.Name)
				continue
			}
			if t := recvBase(fd); !rendered[t] {
				e.errs.add("E0341", a.Pos, "@%s goes on a page's or component's method: %s is a method of %s, which no template renders", a.Name, fd.Name.Name, e.qualifiedName(t)).
					help("put it on a method of the page whose markup shows it, calling %s.%s, or drop @%s", t, fd.Name.Name, a.Name)
			}
		}
	}
}

// qualifiedName is a package-level type's name as messages give it.
func (e *engine) qualifiedName(t string) string {
	if e.pkg != nil {
		return e.pkg.Name() + "." + t
	}
	return t
}

// part writes el as its part: ui.Part of its i-th method, the parts of the
// next inside, then the element.
//
//	ui.Part(p, (*Index).Stats, fallback, func(stats Stats) ui.Node { … return ui.HTML(`<p>…</p>`, …) })
func (w *jsxWriter) part(el *jsxElem, i int) {
	if i == len(el.parts) {
		saved := el.parts
		el.parts = nil
		w.piece([]jsxNode{el})
		el.parts = saved
		return
	}
	m := el.parts[i].m
	q := w.q
	if el.parts[i].binding {
		w.gen(q + "LazyPart(")
		w.partFallback(el, el.parts[i])
		w.gen(", nil, func(ctx context.Context) " + q + "Node {\n")
		sink, ran := w.sink, w.ran
		w.sink, w.ran = &stream{}, nil
		body := w.expr(func() { w.part(el, i+1) })
		stmts := w.sink.p
		w.sink, w.ran = sink, ran
		w.put(stmts)
		w.gen("return ")
		w.put(body)
		w.gen("\n})")
		return
	}
	w.gen(q + "Part(" + m.recv + ", " + m.expr + ", ")
	w.partFallback(el, el.parts[i])
	w.gen(", func(" + m.param + " " + m.typ + ") " + q + "Node {\n")
	sink, ran := w.sink, w.ran
	w.sink, w.ran = &stream{}, nil
	body := w.expr(func() { w.part(el, i+1) })
	stmts := w.sink.p
	w.sink, w.ran = sink, ran
	w.put(stmts)
	w.gen("return ")
	w.put(body)
	w.gen("\n})")
}

// partFallback writes what el's part shows while its method runs, and
// when it fails: its binding's <ui.Fallback>s', else the method's, else
// el empty, marked aria-busy.
func (w *jsxWriter) partFallback(el *jsxElem, pu *partUse) {
	q := w.q
	fallback, failed := pu.m.fallback, pu.m.failed
	if pu.fallback != nil || pu.failed != nil {
		fallback, failed = pu.fallback, pu.failed
	}
	pending := func() {
		switch {
		case fallback != nil && len(fallback.kids) > 0:
			w.piece(fallback.kids)
		case fallback != nil, el.comp != nil:
			w.gen("nil")
		default:
			w.gen(q + "HTML(" + rawString(busyElement(el)) + ")")
		}
	}
	if failed == nil {
		pending()
		return
	}
	w.gen(q + "PartFallback{Pending: ")
	pending()
	w.gen(", Error: ")
	if len(failed.kids) > 0 {
		w.piece(failed.kids)
	} else {
		w.gen("nil")
	}
	w.gen("}")
}

// busyElement is el's tag and its literal attributes, empty, with
// aria-busy: a part's fallback when it has none of its own.
func busyElement(el *jsxElem) string {
	var b strings.Builder
	b.WriteString("<" + el.tag)
	for _, a := range el.attrs {
		switch {
		case a.name == "key" || a.name == "ref" || a.name == "is:inline" || a.name == "aria-busy":
		case a.kind == 's':
			b.WriteString(" " + htmlName(a.name) + `="` + escapeAttr(a.val) + `"`)
		case a.kind == 'b':
			b.WriteString(" " + htmlName(a.name))
		}
	}
	b.WriteString(` aria-busy="true"/>`)
	return b.String()
}

// withoutFallbacks is ns without the <ui.Fallback>s of parts, which render
// nothing where they are written: their attributes' Go is dropped, their
// children written in the parts they stand in for.
func (w *jsxWriter) withoutFallbacks(ns []jsxNode) []jsxNode {
	var out []jsxNode
	for i, n := range ns {
		el, ok := n.(*jsxElem)
		if !ok || el.partFB == nil {
			if out != nil {
				out = append(out, n)
			}
			continue
		}
		if out == nil {
			out = append([]jsxNode{}, ns[:i]...)
		}
		for _, a := range el.attrs {
			if a.kind == 'e' && a.expr.end > a.expr.start {
				w.out.p = append(w.out.p, jsxPiece{keep: a.expr, copy: true, drop: true})
			}
		}
	}
	if out == nil {
		return ns
	}
	return trimBody(out)
}

// markupGo is the Go of markup: its statements' lines and its holes.
func markupGo(src []byte) string {
	var b strings.Builder
	b.WriteString(goParts(src))
	depth, start := 0, 0
	for i, c := range src {
		switch c {
		case '{':
			if depth == 0 {
				start = i + 1
			}
			depth++
		case '}':
			if depth > 0 {
				if depth--; depth == 0 {
					b.WriteString(string(src[start:i]) + "\n")
				}
			}
		}
	}
	return b.String()
}

// invalidSig reports whether a type in sig isn't known (yet).
func invalidSig(sig *types.Signature) bool {
	for _, t := range []*types.Tuple{sig.Params(), sig.Results()} {
		for i := range t.Len() {
			if isInvalid(t.At(i).Type()) || strings.Contains(t.At(i).Type().String(), "invalid type") {
				return true
			}
		}
	}
	return false
}

// strayFallbacks reports the <ui.Fallback for={…}>s of markup whose struct
// defers no method.
func (e *engine) strayFallbacks(f *fileState) {
	var walk func(ns []jsxNode)
	walk = func(ns []jsxNode) {
		for _, n := range ns {
			switch n := n.(type) {
			case *jsxElem:
				if a := attrNamed(n, "for"); a != nil && n.isFallbackFor(f) {
					text := strings.TrimSpace(string(f.src[a.expr.start:a.expr.end]))
					e.errs.add("E0348", f.at(n.start), "<%s for={%s}>: %s defers no method, so nothing stands in for a part", n.tag, text, f.html.recvType).
						help("mark %s @ui.Defer, or drop the fallback", text)
					continue
				}
				walk(n.kids)
			case *jsxBlock:
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
		walk([]jsxNode{t.root})
	}
}

// partBinding is a statement of the markup binding a name to a deferred
// method's result: categories := GetCategories().
type partBinding struct {
	listed bool
	m      *deferMethod
	call   *ast.CallExpr
	id     *ast.Ident
	stmt   span
	el     *jsxElem // the element it is in, its part
	use    *partUse
}

// deferBinding is the statement at s when it binds a name to a deferred
// method's result, or nil.
func (e *engine) deferBinding(f *fileState, s span, methodOf func(*ast.CallExpr) *deferMethod) *partBinding {
	as := e.stmtAssign(f, s)
	if as == nil || as.Tok != token.DEFINE || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
		return nil
	}
	id, ok := as.Lhs[0].(*ast.Ident)
	c, isCall := ast.Unparen(as.Rhs[0]).(*ast.CallExpr)
	if !ok || !isCall {
		return nil
	}
	m := methodOf(c)
	if m == nil && len(c.Args) == 1 {
		// Unwrapped already: Must(GetCategories(ctx)).
		if inner, ok := ast.Unparen(c.Args[0]).(*ast.CallExpr); ok && f.unwrapped[span{f.orig(inner.Pos()), f.orig(inner.End())}] {
			c, m = inner, methodOf(inner)
		}
	}
	if m == nil {
		return nil
	}
	return &partBinding{m: m, call: c, id: id, stmt: s}
}

// bindPart makes a deferred binding's element its part, reporting whether
// the markup uses the name: a binding no markup reads is dropped, its
// method never called. A use outside the element, or in a component's
// prop inside it, is E0344.
func (e *engine) bindPart(f *fileState, b *partBinding) bool {
	m := b.m
	if b.el == nil || b.el.frag {
		e.errs.add("E0350", f.at(b.stmt.start), "@%s: %s binds %s's result in no element, which would be its part", m.attr.Name, b.id.Name, m.name).
			help("put the statement inside the element showing %s", b.id.Name)
		return false
	}
	uses := e.usesOf(f, e.info.Defs[b.id], b.id.Name)
	if len(uses) == 0 {
		kept := f.fixed[:0]
		for _, ed := range f.fixed {
			if ed.start < b.stmt.start || ed.end > b.stmt.end || ed.own != nil {
				kept = append(kept, ed)
			}
		}
		f.fixed = append(kept, edit{start: b.stmt.start, end: b.stmt.end})
		return false
	}
	var props []span
	var tags []string
	var walk func(ns []jsxNode)
	walk = func(ns []jsxNode) {
		for _, n := range ns {
			switch n := n.(type) {
			case *jsxElem:
				if n.comp != nil && !n.isFallbackFor(f) {
					for _, a := range n.attrs {
						switch a.kind {
						case 'e', 'r':
							props, tags = append(props, a.expr), append(tags, n.tag+" "+a.name)
						case 'p':
							for _, p := range a.parts {
								props, tags = append(props, p.expr), append(tags, n.tag+" "+a.name)
							}
						}
					}
				}
				walk(n.kids)
			case *jsxBlock:
				for _, x := range n.bodies {
					walk(x)
				}
				for _, x := range n.cases {
					walk(x)
				}
			}
		}
	}
	walk(b.el.kids)
	ok := true
	for _, u := range uses {
		off := f.orig(u.Pos())
		if off < b.el.start || off >= b.el.end {
			err := e.errs.add("E0344", f.at(off), "@%s: %s is %s's result, whose part is the <%s> it is bound in; it is used outside it", m.attr.Name, b.id.Name, m.name, b.el.tag).
				help("use %s inside the <%s>, or bind it in an element holding every use", b.id.Name, b.el.tag)
			err.Len, ok = len(b.id.Name), false
			continue
		}
		for i, p := range props {
			if off >= p.start && off < p.end {
				tag, attr, _ := strings.Cut(tags[i], " ")
				err := e.errs.add("E0344", f.at(off), "@%s: %s's result goes to <%s %s=…>; defer it in %s instead", m.attr.Name, m.name, tag, attr, lastName(tag)).
					help("give %s a deferred method of its own, or drop @%s", lastName(tag), m.attr.Name)
				err.Len, ok = len(b.id.Name), false
			}
		}
	}
	if !ok {
		return false
	}
	if b.use == nil {
		b.use = &partUse{m: m, binding: true}
		b.el.parts = append(b.el.parts, b.use)
	}
	return true
}

// bindingNamed is the deferred binding a fallback's for={name} names: the
// one named so whose element holds the fallback, else the only one.
func bindingNamed(bound []*partBinding, name string, fb *jsxElem) *partBinding {
	var named []*partBinding
	for _, b := range bound {
		if b.id.Name == name && b.use != nil {
			if b.el != nil && fb.start >= b.el.start && fb.end <= b.el.end {
				return b
			}
			named = append(named, b)
		}
	}
	if len(named) == 1 {
		return named[0]
	}
	return nil
}
