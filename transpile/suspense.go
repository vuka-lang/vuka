package transpile

import (
	"go/ast"
	"go/token"
	"strings"
)

// <ui.Suspense> is a boundary of a stateless page: what its markup reaches
// renders after the page's first bytes, every call in it with it, and is
// sent at once when all of it has returned; its <slot:fallback> shows
// until then, its <slot:error> when any of it fails.
//
//	<ui.Suspense>
//	  <slot:fallback><ui.Skeleton lines={3}/></slot:fallback>
//	  <slot:error>Couldn't load categories.</slot:error>
//	  categories := GetCategories()
//	  <h3>{len(categories)} categories</h3>
//	</ui.Suspense>
//
// is
//
//	ui.Suspense(ui.Skeleton{Lines: 3}, ui.HTML(`Couldn't load categories.`), func(ctx context.Context) ui.Node {
//		categories := sync.OnceValue(func() []Category { return ui.Must(p.GetCategories(ctx)) })
//		return ui.HTML(`<h3>{len(categories())} categories</h3>`, len(categories()))
//	})
//
// Inside a boundary a deferred method is called where the markup calls it:
// the boundary is the part.

// isSuspense reports whether el is a <ui.Suspense> tag of f's target.
func (el *jsxElem) isSuspense(f *fileState) bool {
	return el.comp != nil && f.target != nil && el.tag == f.target.q+"Suspense" && f.target.has("Suspense")
}

// suspenseSlots are a boundary's <slot:fallback> and <slot:error>, and the
// rest of its children: its body.
func suspenseSlots(el *jsxElem) (fallback, failed *jsxElem, body []jsxNode) {
	for _, k := range el.kids {
		if s, ok := k.(*jsxElem); ok && s.isSlot() {
			switch strings.TrimPrefix(s.tag, "slot:") {
			case "fallback":
				fallback = s
				continue
			case "error":
				failed = s
				continue
			}
		}
		body = append(body, k)
	}
	return fallback, failed, trimBody(body)
}

// suspenseErrors reports a boundary's other slots: E0351.
func (e *engine) suspenseErrors(f *fileState, el *jsxElem) {
	for _, a := range el.attrs {
		if a.name == "for" && a.kind == 'e' {
			continue
		}
		e.errs.add("E0351", f.at(a.off), "<%s> takes only for={bindings}: its <slot:fallback> and <slot:error> are its fallbacks", el.tag).
			help("drop %s", a.name)
	}
	seen := map[string]bool{}
	for _, k := range el.kids {
		s, ok := k.(*jsxElem)
		if !ok || !s.isSlot() {
			continue
		}
		name := strings.TrimPrefix(s.tag, "slot:")
		switch {
		case name != "fallback" && name != "error":
			e.errs.add("E0351", f.at(s.start), "<%s> has a <slot:fallback> and a <slot:error>, not <%s>", el.tag, s.tag).
				help("write <slot:fallback>…</slot:fallback> for what shows while it loads, <slot:error>…</slot:error> for a failure")
		case seen[name]:
			e.errs.add("E0351", f.at(s.start), "<%s> has a second <%s>", el.tag, s.tag).help("keep one")
		}
		seen[name] = true
	}
}

// suspense writes a boundary: ui.Suspense(fallback, error, func(ctx) ui.Node { … }).
func (w *jsxWriter) suspense(el *jsxElem) {
	q := w.q
	fallback, failed, body := suspenseSlots(el)
	slot := func(s *jsxElem) {
		if s == nil || len(trimBody(s.kids)) == 0 {
			w.gen("nil")
			return
		}
		w.piece(s.kids)
	}
	w.gen(q + "Suspense(")
	slot(fallback)
	w.gen(", ")
	slot(failed)
	// The markup's function imports context: it has a ctx (htmlParts).
	w.gen(", func(ctx context.Context) " + q + "Node {\n")
	sink, ran := w.sink, w.ran
	w.sink, w.ran = &stream{}, nil
	expr := w.expr(func() { w.piece(body) })
	stmts := w.sink.p
	w.sink, w.ran = sink, ran
	w.put(stmts)
	w.gen("return ")
	w.put(expr)
	w.gen("\n})")
}

// checkSuspense reports f's boundaries' misplaced slots and attributes.
func (e *engine) checkSuspense(f *fileState) {
	var walk func(ns []jsxNode)
	walk = func(ns []jsxNode) {
		for _, n := range ns {
			switch n := n.(type) {
			case *jsxElem:
				if n.isSuspense(f) {
					e.suspenseErrors(f, n)
					e.suspenseBindings(f, n)
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

// suspenseBindings resolves for={a, b} against preceding markup bindings.
func (e *engine) suspenseBindings(f *fileState, el *jsxElem) {
	a := attrNamed(el, "for")
	if a == nil || a.kind != 'e' {
		return
	}
	if f.susBindings == nil {
		f.susBindings = map[int]*jsxElem{}
	}
	stmts := e.markupStmts(f)
	type binding struct {
		st markupStmt
		id *ast.Ident
	}
	bindings := map[string]binding{}
	var names []string
	for _, st := range stmts {
		s := st.b.heads[0]
		if s.start >= el.start || st.el != nil && (el.start < st.el.start || el.end > st.el.end) {
			continue
		}
		as := e.stmtAssign(f, s)
		if as == nil || as.Tok != token.DEFINE {
			continue
		}
		for _, l := range as.Lhs {
			id, ok := l.(*ast.Ident)
			if !ok || id.Name == "_" {
				continue
			}
			if _, exists := bindings[id.Name]; !exists {
				names = append(names, id.Name)
			}
			bindings[id.Name] = binding{st, id}
		}
	}
	text := string(f.src[a.expr.start:a.expr.end])
	seen := map[string]bool{}
	for _, raw := range strings.Split(text, ",") {
		name := strings.TrimSpace(raw)
		b, ok := bindings[name]
		if !ok {
			err := e.errs.add("E0352", f.at(a.expr.start), "<%s for={%s}>: %s is not a binding in scope", el.tag, text, name)
			if near := closestName(name, names); near != "" {
				err.Suggest = near
			}
			err.help("name a preceding := binding in the same enclosing element")
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		at := b.st.b.heads[0].start
		if previous := f.susBindings[at]; previous != nil && previous != el {
			e.errs.add("E0352", f.at(a.expr.start), "%s is listed in two Suspense boundaries", name).help("list a binding in only one boundary")
			continue
		}
		f.susBindings[at] = el
		visited := map[*ast.Ident]bool{}
		var usedInside func(*ast.Ident) bool
		usedInside = func(id *ast.Ident) bool {
			if visited[id] {
				return false
			}
			visited[id] = true
			for _, u := range e.usesOf(f, e.info.Defs[id], id.Name) {
				off := f.orig(u.Pos())
				if off > el.tagEnd && off < el.end {
					return true
				}
				for _, st := range stmts {
					head := st.b.heads[0]
					if off < head.start || off >= head.end {
						continue
					}
					if as := e.stmtAssign(f, head); as != nil {
						for _, lhs := range as.Lhs {
							if dependent, ok := lhs.(*ast.Ident); ok && usedInside(dependent) {
								return true
							}
						}
					}
				}
			}
			return false
		}
		if !usedInside(b.id) {
			f.susWarnings.add("W0351", f.at(a.expr.start), "Suspense waits for %s but shows nothing from it", name)
		}
	}
}
