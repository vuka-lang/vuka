package transpile

import "go/types"

// An element's show={…} shows it while its value holds, and its
// transition={…} animates the change (ui.ShowTransition): show takes a
// bool or an expression of states reading as one, and a transition goes
// with a show on the same element. Either wrong is E0180.

// checkShows checks the show and transition attributes of f's elements,
// once their types are known.
func (e *engine) checkShows(f *fileState) {
	if f.showsChecked || e.info == nil || !f.target.has("VukaState") {
		return
	}
	pending := false
	defer func() { f.showsChecked = !pending }()
	for _, t := range f.jsx {
		walkNodes([]jsxNode{t.root}, func(n jsxNode) {
			el, ok := n.(*jsxElem)
			if !ok || el.comp != nil || el.frag || isComponent(el.tag) {
				return
			}
			var show, tr *jsxAttr
			for _, a := range el.attrs {
				switch a.name {
				case "show":
					show = a
				case "transition":
					tr = a
				}
			}
			if tr != nil && show == nil && tr.kind == 'e' {
				x := e.exprAt(f, tr.expr)
				if x != nil && e.info.TypeOf(x) == nil {
					pending = true
				}
				if x != nil && isTargetNamed(f, e.info.TypeOf(x), "ShowTransition") {
					e.errs.add("E0180", f.at(tr.off), "<%s>'s transition animates its show, and it has none", el.tag).
						help("show it while a state holds: <%s show={open} transition={…}>", el.tag)
				}
			}
			if show == nil {
				return
			}
			if show.kind != 'e' {
				e.errs.add("E0180", f.at(show.off), "<%s show> takes a bool or an expression of states: show={open}", el.tag)
				return
			}
			x := e.exprAt(f, show.expr)
			if x == nil {
				return
			}
			if e.unknownTypes(x) {
				pending = true
				return
			}
			tt := e.info.TypeOf(x)
			if elem, ok := readOf(tt); ok {
				tt = elem
			}
			if b, ok := tt.(*types.Basic); tt != nil && !isInvalid(tt) && (!ok || b.Info()&types.IsBoolean == 0) {
				e.errs.add("E0180", f.at(show.off), "<%s show={%s}>: show takes a bool, and %s is a %s", el.tag, f.src[show.expr.start:show.expr.end], f.src[show.expr.start:show.expr.end], e.display(e.info.TypeOf(x))).
					help("compare it: show={%s != %s}", f.src[show.expr.start:show.expr.end], zeroText(tt))
			}
		})
	}
}

// zeroText is a value of t's to compare with in a message.
func zeroText(t types.Type) string {
	if b, ok := t.Underlying().(*types.Basic); ok {
		switch {
		case b.Info()&types.IsString != 0:
			return `""`
		case b.Info()&types.IsNumeric != 0:
			return "0"
		}
	}
	return "nil"
}
