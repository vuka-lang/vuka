package transpile

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"regexp"
	"strings"
)

// Streams. A loop over a ui.Stream's Rows() that is the only content of its
// element, the container, lowers to the stream's Block of the loop's rows,
// its attributes on the container and the block its content:
//
//	<ul class="log">                          var __lis []ui.Node
//	  for _, m := range messages.Rows() {     for _, m := range c.messages.Rows() {
//	    <li key={m.ID}>{m.Text}</li>             __lis = append(__lis, ui.HTML(`<li key={m.ID}>{m.Text}</li>`, m.ID, m.Text))
//	  }                                       }
//	</ul>                                     __messages := c.messages.Block(__lis)
//	                                          … ui.HTML(`<ul class="log" {__messagesAttrs...}>{__messages}</ul>`, __messages.Attrs(), __messages)
//
// The browser owns the container's children, so the loop must be all it
// holds (E0353), and its rows one keyed element each (E0355) whose
// handlers name no closure the server would keep (E0358) and that hold
// no live component (E0359). Rows() read anywhere else (E0354), a Delete
// of another type than the rows' key (E0356) and a stream a parent could
// set (E0357) are errors too.

var streamHead = regexp.MustCompile(`^for\s+[^{]*?\brange\s+([\w.]+)\.Rows\(\)\s*\{$`)

// streamUse is a stream's container the markup lowered: where its loop's
// head is, and the type of its rows' key.
type streamUse struct {
	head span
	key  types.Type
}

// isStream reports whether t is a ui.Stream: tracked state with a Block
// of rows.
func isStream(t types.Type) bool {
	if t == nil {
		return false
	}
	if p, ok := t.Underlying().(*types.Pointer); ok {
		t = p.Elem()
	}
	ms := types.NewMethodSet(types.NewPointer(t))
	return ms.Lookup(nil, "AssignDirty") != nil && ms.Lookup(nil, "Block") != nil && ms.Lookup(nil, "Rows") != nil
}

// streamLoop is the stream a loop ranges over the Rows() of: its Go (with
// the receiver), its field's name and where its expression is in the
// source; ok is false for any other block, or before types are known.
func (w *jsxWriter) streamLoop(n *jsxBlock) (x, field string, at span, ok bool) {
	if !w.isLoop(n) || w.f.eng.info == nil {
		return "", "", span{}, false
	}
	head := string(w.f.src[n.heads[0].start:n.heads[0].end])
	m := streamHead.FindStringSubmatchIndex(head)
	if m == nil {
		return "", "", span{}, false
	}
	x = head[m[2]:m[3]]
	at = span{n.heads[0].start + m[2], n.heads[0].start + m[3]}
	field = x[strings.LastIndexByte(x, '.')+1:]
	if !strings.Contains(x, ".") && w.f.html != nil && w.f.html.recv != "" {
		x = w.f.html.recv + "." + x // a bare name of the markup is its receiver's field
	}
	t := w.f.eng.typeAt(w.f, w.f.cur.fromOrig(at.start), w.f.cur.fromOrigEnd(at.end))
	if t == nil {
		t = w.shapeField(field)
	}
	return x, field, at, isStream(t)
}

// shapeField is the type of the field name of the file's shape, if any.
func (w *jsxWriter) shapeField(name string) types.Type {
	e, f := w.f.eng, w.f
	if f.html == nil || f.html.shape == nil || e.pkg == nil {
		return nil
	}
	obj, _ := e.pkg.Scope().Lookup(f.html.shape.Type).(*types.TypeName)
	if obj == nil {
		return nil
	}
	st, _ := obj.Type().Underlying().(*types.Struct)
	for i := 0; st != nil && i < st.NumFields(); i++ {
		if st.Field(i).Name() == name {
			return st.Field(i).Type()
		}
	}
	return nil
}

// streamContainer is the loop over a stream an element holds alone.
func (w *jsxWriter) streamContainer(el *jsxElem, kids []jsxNode) *jsxBlock {
	if el.comp != nil || el.frag || !w.final {
		return nil
	}
	var loop *jsxBlock
	for _, k := range kids {
		switch k := k.(type) {
		case *jsxText:
			if strings.TrimSpace(k.val) != "" {
				return nil
			}
		case *jsxBlock:
			if loop != nil {
				return nil
			}
			loop = k
		default:
			return nil
		}
	}
	if loop == nil {
		return nil
	}
	if _, _, _, ok := w.streamLoop(loop); !ok {
		return nil
	}
	return loop
}

// streamElement writes a stream's container: its element, with the
// stream's attributes, holding its block.
func (w *jsxWriter) streamElement(tp *tmpl, el *jsxElem, loop *jsxBlock, indent string, inline bool) {
	x, field, _, _ := w.streamLoop(loop)
	e, f := w.f.eng, w.f
	if w.inLoop {
		e.errs.add("E0353", f.at(loop.heads[0].start), "the loop over %s.Rows() is inside another loop", field).
			help("render the stream's container once, outside any loop: the browser keeps one container's rows")
	}
	if f.streams == nil {
		f.streams = map[string]*streamUse{}
	}
	if u := f.streams[field]; u != nil {
		e.errs.add("E0353", f.at(loop.heads[0].start), "%s.Rows() is ranged over twice", field).
			help("a stream has one container: render its rows in one loop")
	}
	use := &streamUse{head: loop.heads[0]}
	f.streams[field] = use
	w.checkRow(loop, field, use)
	w.checkViewport(el, field, use)
	cp := *el
	cp.kids = nil
	w.container = true
	w.element(tp, &cp, indent, inline)
	start := strings.TrimSuffix(tp.b.String(), "/>")
	tp.b.Reset()
	tp.b.WriteString(start)
	w.streaming = true
	rows := w.block(loop)
	b := w.varName(field)
	attrs := b + ".Attrs()"
	w.stmts(func() { w.gen(b + " := " + x + ".Block(" + rows + ")\n") })
	tp.b.WriteString(" {")
	tp.label(attrs)
	tp.b.WriteString("...}>{")
	tp.hole(attrs, func() { w.gen(b + ".Attrs()") })
	tp.label(b)
	tp.b.WriteString("}</" + el.tag + ">")
	tp.hole(b, func() { w.gen(b) })
}

// checkRow reports a stream row that isn't one keyed element, a handler
// in it the server would have to keep, and a live component in it; it
// records the type of the row's key.
func (w *jsxWriter) checkRow(loop *jsxBlock, field string, use *streamUse) {
	e, f := w.f.eng, w.f
	var row *jsxElem
	for _, k := range flatten(loop.bodies[0]) {
		switch k := k.(type) {
		case *jsxText:
			if strings.TrimSpace(k.val) == "" {
				continue
			}
		case *jsxElem:
			if row == nil && k.comp == nil {
				row = k
				continue
			}
			if row == nil && k.comp != nil && k.comp.stateful {
				e.errs.add("E0359", f.at(k.start), "a row of %s is the live component <%s>", field, k.tag).
					help("render a stateless shape in the row, and push to the owner's methods: ui.JS.Push(Remove, m.ID)")
				return
			}
		}
		e.errs.add("E0355", f.at(loop.heads[0].start), "a row of %s isn't one element", field).
			help("render each row as one element keyed by the record: <li key={m.ID}>…</li>")
		return
	}
	if row == nil {
		e.errs.add("E0355", f.at(loop.heads[0].start), "a row of %s isn't one element", field).
			help("render each row as one element keyed by the record: <li key={m.ID}>…</li>")
		return
	}
	key := attrNamed(row, "key")
	if key == nil || key.kind != 'e' {
		e.errs.add("E0355", f.at(row.start), "a row of %s has no key", field).
			help("key the row's element by its record: <%s key={m.ID}>", row.tag)
		return
	}
	use.key = e.typeAt(f, f.cur.fromOrig(key.expr.start), f.cur.fromOrigEnd(key.expr.end))
	var walk func(ns []jsxNode)
	walk = func(ns []jsxNode) {
		for _, n := range ns {
			switch n := n.(type) {
			case *jsxElem:
				if n.comp != nil && n.comp.stateful {
					e.errs.add("E0359", f.at(n.start), "a row of %s renders the live component <%s>", field, n.tag).
						help("render a stateless shape in the row, and push to the owner's methods: ui.JS.Push(Remove, m.ID)")
					continue
				}
				for _, a := range n.attrs {
					w.checkRowAttr(n, a, field)
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
	walk([]jsxNode{row})
}

// checkRowAttr reports a row's attribute holding a function: a handler the
// session would have to keep for the row.
func (w *jsxWriter) checkRowAttr(el *jsxElem, a *jsxAttr, field string) {
	e, f := w.f.eng, w.f
	if a.kind != 'e' || !a.event && el.comp == nil {
		return
	}
	bad := a.stmts
	if !bad {
		t := e.typeAt(f, f.cur.fromOrig(a.expr.start), f.cur.fromOrigEnd(a.expr.end))
		_, bad = types.Unalias(typeOrNil(t)).Underlying().(*types.Signature)
		if !bad {
			text := strings.TrimSpace(string(f.src[a.expr.start:a.expr.end]))
			bad = strings.HasPrefix(text, "func(") || strings.HasPrefix(text, "func (")
		}
	}
	if bad {
		e.errs.add("E0358", f.at(a.off), "%s in a row of %s is a handler the server would have to keep for the row", a.name, field).
			help("push a method of the stream's owner with the row's key: %s={ui.JS.Push(Remove, m.ID)}", a.name)
	}
}

// isViewport reports whether an attribute is a viewport event: a stream
// container's onViewportTop or onViewportBottom.
func isViewport(name string) bool {
	return strings.EqualFold(name, "onViewportTop") || strings.EqualFold(name, "onViewportBottom")
}

// checkViewportAttrs reports a viewport event on an element that isn't a
// stream's container (E0360): only a container has edge rows whose keys
// the event sends.
func (w *jsxWriter) checkViewportAttrs(el *jsxElem) {
	if el.comp != nil || !w.final {
		return
	}
	for _, a := range el.attrs {
		if a.event && isViewport(a.name) {
			w.f.eng.errs.add("E0360", w.f.at(a.off), "%s is on <%s>, which isn't a stream's container: the event sends the key of a container's edge row", a.name, el.tag).
				help("put it on the element whose only content is the loop over a stream's Rows(): <ul %s={More}>for _, m := range messages.Rows() {…}</ul>", a.name)
		}
	}
}

// checkViewport reports a container's viewport handler that can't take
// what the event sends (E0361): its value is the edge row's key, so the
// handler takes the key's type (or its text, a string) once, or nothing,
// beside a context and what the app injects, and returns nothing or an
// error.
func (w *jsxWriter) checkViewport(el *jsxElem, field string, use *streamUse) {
	e, f := w.f.eng, w.f
	for _, a := range el.attrs {
		if !a.event || !isViewport(a.name) || a.kind != 'e' || a.stmts {
			continue
		}
		t := e.typeAt(f, f.cur.fromOrig(a.expr.start), f.cur.fromOrigEnd(a.expr.end))
		sig, _ := types.Unalias(typeOrNil(t)).Underlying().(*types.Signature)
		if sig == nil {
			continue // JS commands, or not known
		}
		key := "string" // the key's text, when its type isn't known
		if use.key != nil {
			key = types.TypeString(use.key, types.RelativeTo(e.pkg))
		}
		bad := ""
		values := 0
		for i := 0; i < sig.Params().Len(); i++ {
			pt := sig.Params().At(i).Type()
			switch pt.Underlying().(type) {
			case *types.Pointer, *types.Interface:
				continue // a context, or what the app injects
			case *types.Struct:
				if n, ok := types.Unalias(pt).(*types.Named); ok && n.Obj().Name() == "Event" {
					values++
					continue
				}
				bad = fmt.Sprintf("takes %s, a form, and the event has none", types.TypeString(pt, types.RelativeTo(e.pkg)))
			default:
				values++
				if use.key != nil && !types.Identical(pt, use.key) && !isString(pt) {
					bad = fmt.Sprintf("takes %s, and the edge row's key is %s", types.TypeString(pt, types.RelativeTo(e.pkg)), key)
				}
			}
		}
		if bad == "" && values > 1 {
			bad = "takes more than one value, and the event sends one: the edge row's key"
		}
		if r := sig.Results(); bad == "" && (r.Len() > 1 || r.Len() == 1 && !types.Identical(r.At(0).Type(), errorType)) {
			bad = "returns " + r.String() + ": a handler returns nothing or an error"
		}
		if bad != "" {
			e.errs.add("E0361", f.at(a.off), "the %s handler of the container of %s %s", a.name, field, bad).
				help("take the edge row's key: func (c *Page) More(ctx context.Context, last %s) error", key)
		}
	}
}

func typeOrNil(t types.Type) types.Type {
	if t == nil {
		return types.Typ[types.Invalid]
	}
	return t
}

// checkStreams reports, in the lowered package, a stream a parent could
// set or one bound to the URL or the browser (E0357), Rows() read where
// it isn't a container's loop (E0354), and a Delete of another type than
// the rows' key (E0356).
func (e *engine) checkStreams() {
	for _, f := range e.vuka {
		if f.ast == nil {
			continue
		}
		if f.html != nil && f.html.shape != nil {
			e.checkStreamFields(f)
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
			if !ok || !f.lowerable(sel.Pos()) {
				return true
			}
			s := e.info.Selections[sel]
			if s == nil || s.Kind() != types.MethodVal || !isStream(s.Recv()) {
				return true
			}
			fieldName := ""
			if fs, ok := ast.Unparen(sel.X).(*ast.SelectorExpr); ok {
				fieldName = fs.Sel.Name
			} else if id, ok := ast.Unparen(sel.X).(*ast.Ident); ok {
				fieldName = id.Name
			}
			switch sel.Sel.Name {
			case "Rows":
				off := f.orig(call.Pos())
				if u := f.streams[fieldName]; u != nil && off >= u.head.start && off < u.head.end {
					return true
				}
				e.errs.add("E0354", f.at(off), "%s.Rows() is read outside its container's loop: it is what is queued, not what the browser shows", fieldName).
					help("keep counts in fields beside the stream, changed where the records change; Rows() is only a container's loop")
			case "Delete":
				u := f.streams[fieldName]
				if u == nil || u.key == nil {
					return true
				}
				for _, a := range call.Args {
					at := e.info.Types[a]
					if at.Type == nil || types.Identical(at.Type, u.key) || at.Value != nil && keyConst(at.Value, u.key) {
						continue
					}
					e.errs.add("E0356", f.at(f.orig(a.Pos())), "%s.Delete takes the rows' key, of type %s, not %s", fieldName, u.key, at.Type).
						help("pass the key the row's key attribute renders: the browser matches rows by its text")
				}
			}
			return true
		})
	}
}

// checkStreamFields reports the shape's stream fields a parent could set,
// or bound to the URL or the browser.
func (e *engine) checkStreamFields(f *fileState) {
	for _, d := range f.ast.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.TYPE {
			continue
		}
		for _, sp := range g.Specs {
			ts, ok := sp.(*ast.TypeSpec)
			if !ok || ts.Name.Name != f.html.shape.Type {
				continue
			}
			st, _ := ts.Type.(*ast.StructType)
			if st == nil {
				continue
			}
			for _, fl := range st.Fields.List {
				if !isStream(e.info.TypeOf(fl.Type)) {
					continue
				}
				for _, n := range fl.Names {
					switch {
					case n.IsExported():
						e.errs.add("E0357", f.at(f.orig(n.Pos())), "%s is an exported ui.Stream: a parent can't own rows the server forgets", n.Name).
							help("make it unexported: %s", strings.ToLower(n.Name[:1])+n.Name[1:])
					case fl.Tag != nil && (strings.Contains(fl.Tag.Value, `bind:"`) || strings.Contains(fl.Tag.Value, `url:"`)) || boundInSource(f, f.orig(n.Pos())):
						e.errs.add("E0357", f.at(f.orig(n.Pos())), "%s is a ui.Stream bound to the browser or the URL: neither can hold rows the server forgets", n.Name).
							help("keep the stream unbound, and bind a field beside it")
					}
				}
			}
		}
	}
}

// boundInSource reports whether the field declared at off carries @ui.Bind
// or @ui.URL in the source.
func boundInSource(f *fileState, off int) bool {
	if off < 0 || off >= len(f.src) {
		return false
	}
	line := f.src[off:]
	if i := strings.IndexByte(string(line), '\n'); i >= 0 {
		line = line[:i]
	}
	return strings.Contains(string(line), "@ui.Bind") || strings.Contains(string(line), "@ui.URL")
}

// keyConst reports whether a constant argument is a value of the key's
// type: an integer constant for an integer key, a string for a string.
func keyConst(v constant.Value, key types.Type) bool {
	b, ok := key.Underlying().(*types.Basic)
	if !ok {
		return false
	}
	switch v.Kind() {
	case constant.Int:
		return b.Info()&types.IsInteger != 0
	case constant.String:
		return b.Info()&types.IsString != 0
	}
	return false
}
