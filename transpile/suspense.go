package transpile

import "strings"

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
		e.errs.add("E0351", f.at(a.off), "<%s> takes no attributes: its <slot:fallback> and <slot:error> are its fallbacks", el.tag).
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
