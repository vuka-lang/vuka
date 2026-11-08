package transpile

// A component tag taking show={…} with a lazy attribute its props don't
// declare — <Modal show={open} lazy> — renders its children the first time
// show holds: they go in a ui.Lazy following the same show, Bare, so the
// component shows and hides them as before (its transitions its own).
// lazy="each" renders them again each time it shows; a <slot:loading> the
// props don't declare is what shows until they arrive.

// lazyAttr takes c's lazy attribute out of attrs when its props have no
// field of that name and the JSX target has ui.Lazy, or says why it is
// wrong.
func (e *engine) lazyAttr(c *jsxComp, names []string, attrs []*jsxAttr) ([]*jsxAttr, int, string) {
	c.lazy, c.lazyShow, c.lazyLoad = nil, nil, nil
	if c.elem || bindName(names, "lazy") >= 0 || !c.target.has("Lazy") {
		return attrs, 0, ""
	}
	for i, a := range attrs {
		if a.name != "lazy" {
			continue
		}
		if !(a.kind == 'b' || a.kind == 's' && a.val == "each") {
			return attrs, a.off, "<" + c.el.tag + " lazy> takes no value, or \"each\" (lazy=\"each\": the children render again each time it shows)"
		}
		c.lazy = a
		return append(attrs[:i:i], attrs[i+1:]...), 0, ""
	}
	return attrs, 0, ""
}

// lazyOpen opens the ui.Lazy a lazy component tag's children go in.
func (w *jsxWriter) lazyOpen(c *jsxComp) {
	q := w.q
	w.gen(q + "Lazy(" + q + "LazyProps{Bare: true, When: ")
	w.attrValue(c.lazyShow.attr)
	if c.lazy.kind == 's' {
		w.gen(", Each: true")
	}
	if c.lazyLoad != nil && hasContent(c.lazyLoad.kids) {
		w.gen(", Loading: ")
		w.piece(c.lazyLoad.kids)
	}
	w.gen(", Children: ")
}
