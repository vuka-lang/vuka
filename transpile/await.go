package transpile

import (
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// <ui.Await key="orders"> is the section of a live component's markup
// showing one of its deferred groups: its <slot:pending> while the group
// loads, its <slot:error> once the loader failed, else its content.
//
//	<ui.Await key="orders">
//	  <slot:pending><ui.Skeleton lines={3}/></slot:pending>
//	  <slot:error let={err}>Couldn't load the orders: {err.Error()}</slot:error>
//	  <p>{len(orders)} orders</p>
//	</ui.Await>
//
// is
//
//	ui.Await{Of: o, Key: "orders", Pending: ui.Skeleton{Lines: 3}, Error: func(err error) ui.Node {
//		return ui.HTML(`Couldn't load the orders: {err.Error()}`, err.Error())
//	}, Children: func() ui.Node {
//		return ui.HTML(`<p>{len(o.orders)} orders</p>`, len(o.orders))
//	}}

// isAwait reports whether el is a <ui.Await> tag of f's target.
func (el *jsxElem) isAwait(f *fileState) bool {
	return el.comp != nil && f.target != nil && el.tag == f.target.q+"Await" && f.target.has("Await")
}

// awaitSlots are a section's <slot:pending> and <slot:error>, and the rest
// of its children: what shows once the group has loaded.
func awaitSlots(el *jsxElem) (pending, failed *jsxElem, body []jsxNode) {
	for _, k := range el.kids {
		if s, ok := k.(*jsxElem); ok && s.isSlot() {
			switch strings.TrimPrefix(s.tag, "slot:") {
			case "pending":
				pending = s
				continue
			case "error":
				failed = s
				continue
			}
		}
		body = append(body, k)
	}
	return pending, failed, trimBody(body)
}

// await writes a section: ui.Await{Of: o, Key: "orders", Pending: …,
// Error: func(err error) ui.Node { … }, Children: func() ui.Node { … }}.
func (w *jsxWriter) await(el *jsxElem) {
	pending, failed, body := awaitSlots(el)
	of := "nil"
	if h := w.f.html; h != nil && h.recv != "" {
		of = h.recv
	}
	w.gen(w.q + "Await{Of: " + of + ", Key: ")
	switch a := attrNamed(el, "key"); {
	case a != nil && a.kind == 'e':
		w.attrValue(a)
	case a != nil:
		w.gen(strconv.Quote(a.val))
	default:
		w.gen(`""`)
	}
	if pending != nil && hasContent(pending.kids) {
		w.gen(", Pending: ")
		w.piece(pending.kids)
	}
	if failed != nil {
		w.gen(", Error: ")
		w.slotFunc(failed.lets, []string{"error"}, func() { w.content(failed.kids) })
	}
	if hasContent(body) {
		w.gen(", Children: ")
		w.slotFunc(nil, nil, func() { w.piece(body) })
	}
	w.gen("}")
}

// content writes ns as piece does, or nil when they hold nothing.
func (w *jsxWriter) content(ns []jsxNode) {
	if hasContent(ns) {
		w.piece(ns)
		return
	}
	w.gen("nil")
}

// checkAwait reports f's sections outside a live component's markup, their
// misplaced attributes and slots (E0370; a slot it doesn't have, E0106),
// and a key naming no group the component defers (E0325).
func (e *engine) checkAwait(f *fileState) {
	var walk func(ns []jsxNode)
	walk = func(ns []jsxNode) {
		for _, n := range ns {
			switch n := n.(type) {
			case *jsxElem:
				if n.isAwait(f) {
					e.awaitErrors(f, n)
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

func (e *engine) awaitErrors(f *fileState, el *jsxElem) {
	h := f.html
	if h == nil || h.recvType == "" || e.liveSchema(h.recvType) == nil {
		what := "this markup has no component"
		if h != nil && h.recvType != "" {
			what = h.recvType + " isn't live"
		}
		e.errs.add("E0370", f.at(el.start), "<%s> shows a deferred group of a live component, and %s", el.tag, what).
			help("embed %sLive in the component, defer the group's fields with @%sDefer(\"group\") and load them with @%sLoads(\"group\")", f.target.q, f.target.q, f.target.q)
		return
	}
	groups := e.deferredGroups(h.recvType)
	key := attrNamed(el, "key")
	for _, a := range el.attrs {
		if a != key {
			e.errs.add("E0370", f.at(a.off), "<%s> takes only key, the group it shows: its <slot:pending> and <slot:error> are what shows meanwhile", el.tag).
				help("drop %s", a.name)
		}
	}
	switch {
	case key == nil:
		err := e.errs.add("E0370", f.at(el.start), "<%s> needs a key: the deferred group it shows", el.tag)
		if len(groups) > 0 {
			err.suggest(`<` + el.tag + ` key="` + lowerFirst(groups[0]) + `">`)
		} else {
			err.help(`write <%s key="group">, the group a field marked @%sDefer("group") loads in`, el.tag, f.target.q)
		}
	case key.kind == 'b' || key.kind == 'p':
		e.errs.add("E0370", f.at(key.off), "<%s>'s key is the group's name", el.tag).help(`write key="group" or key={expression}`)
	case key.kind == 's' && !token.IsIdentifier(key.val):
		e.errs.add("E0370", f.at(key.off), "<%s key=%q>: a group is a name", el.tag, key.val).help(`write the group's name: key="orders"`)
	case key.kind == 's' && !hasGroup(groups, key.val):
		err := e.errs.add("E0325", f.at(key.off), "%s defers no group %q", h.recvType, key.val)
		var names []string
		for _, g := range groups {
			names = append(names, lowerFirst(g))
		}
		if s := closestName(key.val, names); s != "" {
			err.suggest(strconv.Quote(s))
		} else {
			err.help(`defer a field to it, @%sDefer(%q), or name a group %s defers`, f.target.q, key.val, h.recvType)
		}
	}
	seen := map[string]bool{}
	for _, k := range el.kids {
		s, ok := k.(*jsxElem)
		if !ok || !s.isSlot() {
			continue
		}
		name := strings.TrimPrefix(s.tag, "slot:")
		switch {
		case name != "pending" && name != "error":
			e.errs.add("E0106", f.at(s.start), "%s", unknownSlot(el.tag, name, []string{"pending", "error"}))
		case seen[name]:
			e.errs.add("E0370", f.at(s.start), "<%s> has a second <%s>", el.tag, s.tag).help("keep one")
		case name == "pending" && len(s.lets) > 0:
			e.errs.add("E0370", f.at(s.lets[0].at.start), "<slot:pending> takes no values: let goes on <slot:error>, which takes the error").help("drop the let")
		case name == "error" && len(s.lets) > 1:
			e.errs.add("E0370", f.at(s.lets[1].at.start), "<slot:error> passes one value, the error: let={%s}", s.lets[0].name).help("name only the error")
		}
		seen[name] = true
	}
}

// hasGroup reports whether groups, as ui names them (Orders), hold the
// group key names.
func hasGroup(groups []string, key string) bool {
	for _, g := range groups {
		if g == groupName(key) {
			return true
		}
	}
	return false
}

// deferredGroups are the groups the fields of the live type typeName defer
// to, as ui names them: @ui.Defer("orders") is Orders, a bare @ui.Defer
// the field's name.
func (e *engine) deferredGroups(typeName string) []string {
	si := e.liveSchema(typeName)
	if si == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, a := range si.file.attrs {
		if !a.Field || a.structOf != typeName || lastName(a.Name) != "Defer" || len(a.fieldNames) == 0 {
			continue
		}
		g := a.fieldNames[0]
		if s, err := strconv.Unquote(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(a.Args, "("), ")"))); err == nil && s != "" {
			g = s
		}
		seen[groupName(g)] = true
	}
	groups := make([]string, 0, len(seen))
	for g := range seen {
		groups = append(groups, g)
	}
	sort.Strings(groups)
	return groups
}

// groupName is a group as ui names it: its first letter upper case.
func groupName(g string) string { return strings.ToUpper(g[:1]) + g[1:] }
