package transpile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"reflect"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
)

// Fallthrough (RFC components-seamless §5): what a caller writes on a
// component's tag and no prop claims goes to the component's forward
// element — its markup's one top-level element, or the one marked with a
// bare forward attribute:
//
//	<div class="rounded-xl border">{Children}</div>
//	<label>{Label} <input class="h-10" forward/></label>
//
// A .vuka.html component whose struct is its own and declares no Attrs
// (which takes everything, as it always has) gets a field
//
//	Fallthrough ui.Fallthrough `vuka:"forward=div,own=onclick"`
//
// its tag names the forward element — or why there is none: several roots
// (?<div>|<p>), none that is an element (-{Children}) — and the attributes
// the element sets that a caller can't replace (its events, its style, its
// bind:value). The forward element takes the field: its class merged with
// the caller's, its own attributes the caller sets left out, the caller's
// spread after its name:
//
//	<div {Fallthrough.Attrs()...} class={Fallthrough.Merge("rounded-xl border")}>
//
// A tag's caller (bindFields) puts there its class, the HTML, ARIA and
// data attributes, the DOM events, a bind:value and a ref no prop takes.
//
// A Class prop tagged vuka:"class" is the caller's class, for a component
// computing with it: its use in a class attribute is merged, the caller's
// classes last (class={"base " + Class} is class={ui.MergeClasses("base ",
// Class)}), and a tagged Class no class attribute uses is E0123.

// fallField is the field a component's caller's fallthrough is in.
const fallField = "Fallthrough"

// forwardParse is the target the markup is read with before its file is
// compiled: one that has what the parser asks of a target.
var forwardParse = map[string]bool{"VukaState": true, "On": true, "FormElement": true, "Model": true, "ModelID": true, "FieldsetAttrs": true}

// forwardPlan is what a template's fallthrough writes into its compiled
// text: insertions and blanked spans, in the file's offsets.
type forwardPlan struct {
	ins    []insertion
	blanks []span
}

// forwardMode is how a template's struct takes what its tag passes on.
type forwardMode struct {
	fall       bool   // the struct gets the Fallthrough field
	why        string // why it doesn't, for a forward marker: E0137
	classTaken bool   // a Class prop takes the caller's class
	classTag   *ShapeField
}

// shapeForward is how the struct sh takes what its tag passes on: fall
// says whether its target has ui.Fallthrough.
func shapeForward(sh *Shape, fall bool, ui string) forwardMode {
	var m forwardMode
	for i, fl := range sh.Fields {
		if fl.Role != Prop {
			continue
		}
		switch {
		case fl.Name == "Attrs" && strings.Contains(fl.Type, "Attr"), fl.Name == "Attributes":
			m.why = sh.Type + " declares " + fl.Name + ", which takes the attributes its tag passes on, wherever its markup spreads them ({" + fl.Name + "...})"
		case fl.Name == fallField:
			m.why = sh.Type + " declares a field " + fallField + " of its own"
		case fl.Name == "Class":
			m.classTaken = true
			if vukaFlag(fl.Tag, "class") {
				m.classTag = &sh.Fields[i]
			}
		}
	}
	switch {
	case m.why != "":
	case sh.File != "":
		m.why = sh.Type + " is declared in " + sh.File + ": a component passes its tag's attributes on when the template declares its struct"
	case !fall:
		m.why = "the JSX target " + strings.TrimSuffix(ui, ".") + " has no Fallthrough: update it"
	default:
		m.fall = true
	}
	return m
}

// vukaFlag reports whether a struct tag's vuka options hold name alone:
// `vuka:"class"`.
func vukaFlag(tag, name string) bool {
	for _, v := range strings.Split(reflect.StructTag(tag).Get("vuka"), ",") {
		if strings.TrimSpace(v) == name {
			return true
		}
	}
	return false
}

// planForward reads the markup (at base in the file) and plans its
// forward element's edits and the Fallthrough field, inserted at
// fieldAt; pos is a file offset's position, for errors.
func planForward(markup string, base int, sh *Shape, m forwardMode, ui string, pos func(int) token.Position) (forwardPlan, *Error) {
	var plan forwardPlan
	trees, ok := parseForward(markup)
	if !ok {
		return plan, nil // the compile reports the markup's error
	}
	at := func(off int) int { return base + off - 2 }
	fail := func(off int, msg, help string) *Error {
		return &Error{Code: "E0137", Pos: pos(at(off)), Msg: msg, Help: help}
	}
	root := trees[len(trees)-1].root
	recv := sh.Recv

	// The class a Class tagged vuka:"class" is: merged in each class
	// attribute using it.
	if m.classTag != nil {
		uses := 0
		for _, t := range trees {
			walkJSX(t.root, func(el *jsxElem) {
				for _, a := range el.attrs {
					if (a.name == "class" || a.name == "className") && a.kind == 'e' {
						if text, ok := mergeClassUse(markup[a.expr.start-2:a.expr.end-2], recv, ui); ok {
							uses++
							if text != "" {
								plan.blanks = append(plan.blanks, span{at(a.expr.start), at(a.expr.end)})
								plan.ins = append(plan.ins, insertion{at(a.expr.start), text})
							}
						}
					}
				}
			})
		}
		if uses == 0 {
			return plan, &Error{Code: "E0123", Pos: token.Position{Line: m.classTag.Line, Column: 1},
				Msg:  sh.Type + "'s Class is tagged vuka:\"class\" and no class attribute uses it: a caller's class={…} would be lost",
				Help: "merge it into the class of the element it styles: class={\"base \" + Class}; or drop the tag, and the caller's class falls through to the forward element"}
		}
	}

	var markers []*jsxElem
	var loopMarker *jsxElem
	var walk func(ns []jsxNode, loop bool)
	walk = func(ns []jsxNode, loop bool) {
		for _, n := range ns {
			switch n := n.(type) {
			case *jsxElem:
				if forwardAttr(n) != nil {
					markers = append(markers, n)
					if loop && loopMarker == nil {
						loopMarker = n
					}
				}
				walk(n.kids, loop)
			case *jsxBlock:
				inLoop := loop
				if len(n.heads) > 0 {
					head := strings.TrimSpace(markup[max(n.heads[0].start-2, 0):max(n.heads[0].end-2, 0)])
					inLoop = loop || strings.HasPrefix(head, "for ") || head == "for"
				}
				for _, b := range n.bodies {
					walk(b, inLoop)
				}
				for _, b := range n.cases {
					walk(b, loop)
				}
			}
		}
	}
	walk([]jsxNode{root}, false)
	for _, t := range trees[:len(trees)-1] {
		var in []*jsxElem
		walkJSX(t.root, func(el *jsxElem) {
			if forwardAttr(el) != nil {
				in = append(in, el)
			}
		})
		if len(in) > 0 {
			return plan, fail(in[0].start, "forward marks an element of "+sh.Type+"'s markup, not one of markup in a Go expression or a markup func: what its tag passes on goes to one element, rendered once",
				"mark an element of the template's markup itself")
		}
	}
	for _, el := range markers {
		if a := forwardAttr(el); a.kind != 'b' {
			return plan, fail(a.off, "forward takes no value: it marks the element "+sh.Type+"'s tag passes its attributes on to", "write it bare: <"+el.tag+" forward>")
		}
	}
	switch {
	case len(markers) > 1:
		var names []string
		for _, el := range markers {
			names = append(names, "<"+el.tag+"> (line "+strconv.Itoa(pos(at(el.start)).Line)+")")
		}
		return plan, fail(markers[1].start, sh.Type+" marks two forward elements, "+strings.Join(names, " and ")+": what its tag passes on goes to one",
			"keep forward on the element a caller's class and attributes belong on")
	case loopMarker != nil:
		return plan, fail(loopMarker.start, "forward marks <"+loopMarker.tag+"> inside a for loop: it renders once per item, and what "+sh.Type+"'s tag passes on goes to one element",
			"mark an element outside the loop")
	case !m.fall && len(markers) > 0:
		return plan, fail(markers[0].start, "forward marks <"+markers[0].tag+">, and "+m.why, "drop forward")
	case !m.fall:
		return plan, nil
	}
	for _, el := range markers {
		a := forwardAttr(el)
		plan.blanks = append(plan.blanks, span{at(a.off), at(a.off + len(a.name))})
	}

	// The forward element: the one marked, else the markup's one root.
	var fwd *jsxElem
	var roots []string
	if len(markers) == 1 {
		fwd = markers[0]
	} else {
		var elems []*jsxElem
		for _, n := range root.kids {
			switch n := n.(type) {
			case *jsxText:
				if strings.TrimSpace(n.val) != "" {
					roots = append(roots, "text")
				}
			case *jsxHole:
				roots = append(roots, holeName(markup[n.expr.start-2:n.expr.end-2]))
			case *jsxBlock:
				if !n.stmt {
					roots = append(roots, blockName(n, markup))
				}
			case *jsxElem:
				switch {
				case n.isAsset():
				case strings.HasPrefix(n.tag, "slot:"):
					roots = append(roots, "<"+n.tag+">")
				default:
					elems = append(elems, n)
					roots = append(roots, "<"+n.tag+">")
				}
			}
		}
		if len(elems) == 1 && len(roots) == 1 {
			fwd = elems[0]
		}
	}
	var forward string
	var own []string
	switch {
	case fwd != nil && isComponent(fwd.tag):
		forward = "<" + fwd.tag + ">"
	case fwd != nil:
		forward = fwd.tag
		own = forwardElement(&plan, fwd, markup, m, at)
	case len(roots) == 0:
		forward = "-"
	case len(roots) == 1:
		forward = "-" + roots[0]
	default:
		forward = "?" + strings.Join(roots, "|")
	}
	tag := "forward=" + forward
	if len(own) > 0 {
		tag += ",own=" + strings.Join(own, "|")
	}
	plan.ins = append(plan.ins, insertion{sh.open, fallField + " " + ui + "Fallthrough `vuka:" + strconv.Quote(tag) + "`; "})
	return plan, nil
}

// parseForward reads a template's markup as the compiler will.
func parseForward(markup string) (trees []*jsxTree, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			if _, isJSX := r.(jsxError); !isJSX {
				panic(r)
			}
			trees, ok = nil, false
		}
	}()
	src := []byte("<>" + markup + "</>")
	var errs ErrorList
	f := &fileState{src: src, lines: newLineIndex(src), html: &htmlFile{recv: "p", src: src, lines: newLineIndex(src)}, target: &jsxTarget{names: forwardParse}}
	p := &jsxParser{f: f, src: src, errs: &errs}
	p.tree(0)
	if len(errs) > 0 || len(p.trees) == 0 {
		return nil, false
	}
	return p.trees, true
}

// forwardAttr is el's forward marker, or nil.
func forwardAttr(el *jsxElem) *jsxAttr {
	for _, a := range el.attrs {
		if a.name == "forward" && !a.pass {
			return a
		}
	}
	return nil
}

// holeName is a hole as a message names it: {Children}, or {…}.
func holeName(expr string) string {
	if e := strings.TrimSpace(expr); token.IsIdentifier(e) {
		return "{" + e + "}"
	}
	return "{…}"
}

func blockName(n *jsxBlock, markup string) string {
	if n.m != nil {
		return "a match"
	}
	if len(n.heads) > 0 {
		head := strings.TrimSpace(markup[max(n.heads[0].start-2, 0):max(n.heads[0].end-2, 0)])
		switch {
		case strings.HasPrefix(head, "for"):
			return "a for loop"
		case strings.HasPrefix(head, "switch"):
			return "a switch"
		}
	}
	return "an if"
}

// forwardKeeps are the attributes a forward element keeps whatever its
// caller passes: what the compiler gives meaning to. A caller setting one
// is E0139.
var forwardKeeps = map[string]bool{"style": true, "ref": true, "key": true, "for": true, "form": true, "model": true, "value": true,
	"checked": true, "selected": true, "show": true, "transition": true, "lazy": true, "innerhtml": true, "slot": true, "on": true}

// forwardElement plans the forward element's edits: its own attributes a
// caller may replace given ui.Fallthrough.Or, its class merged, the
// caller's attributes spread after its name. It returns the attributes
// it keeps, lower-cased, for the Fallthrough field's tag.
func forwardElement(plan *forwardPlan, el *jsxElem, markup string, m forwardMode, at func(int) int) []string {
	var own []string
	class := false
	for _, a := range el.attrs {
		name := strings.ToLower(a.name)
		switch {
		case a.name == "forward":
		case a.kind == 'r':
		case a.name == "class" || a.name == "className":
			class = true
			if m.classTaken {
				continue
			}
			switch a.kind {
			case 's', 'p':
				plan.blanks = append(plan.blanks, span{at(a.quote.start), at(a.quote.end)})
				plan.ins = append(plan.ins, insertion{at(a.quote.start), "{" + fallField + ".Merge(" + classPieces(a, markup) + ")}"})
			case 'e':
				plan.ins = append(plan.ins, insertion{at(a.expr.start), fallField + ".Merge("}, insertion{at(a.expr.end), ")"})
			}
		case strings.HasPrefix(a.name, "class:"):
			class = true
		case strings.HasPrefix(a.name, "style:"):
			own = appendOnce(own, "style")
		case a.event || isEvent(a.name) || strings.HasPrefix(a.name, "bind:") || a.pass || a.omit || a.mfn || forwardKeeps[name] || strings.Contains(a.name, ":"):
			own = appendOnce(own, strings.TrimPrefix(name, passPrefix))
		case a.kind == 's':
			plan.blanks = append(plan.blanks, span{at(a.quote.start), at(a.quote.end)})
			plan.ins = append(plan.ins, insertion{at(a.quote.start), "{" + fallField + ".Or(" + strconv.Quote(a.name) + ", " + strconv.Quote(a.val) + ")}"})
		case a.kind == 'b':
			plan.ins = append(plan.ins, insertion{at(a.off + len(a.name)), "={" + fallField + ".Or(" + strconv.Quote(a.name) + ", true)}"})
		case a.kind == 'e':
			plan.ins = append(plan.ins, insertion{at(a.expr.start), fallField + ".Or(" + strconv.Quote(a.name) + ", "}, insertion{at(a.expr.end), ")"})
		default:
			own = appendOnce(own, name)
		}
	}
	spread := "Attrs"
	if !class && !m.classTaken {
		spread = "All"
	} else if !m.classTaken && !hasAttr(el, "class") && !hasAttr(el, "className") {
		// class:x toggles and no class: the caller's is the class they toggle on
		plan.ins = append(plan.ins, insertion{at(el.tagEnd), " class={" + fallField + ".Merge()}"})
	}
	plan.ins = append(plan.ins, insertion{at(el.tagEnd), " {" + fallField + "." + spread + "()...}"})
	return own
}

func hasAttr(el *jsxElem, name string) bool {
	for _, a := range el.attrs {
		if a.name == name {
			return true
		}
	}
	return false
}

func appendOnce(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// classPieces is a quoted class's text and holes as Merge's arguments:
// "card {Size}" is "card ", Size.
func classPieces(a *jsxAttr, markup string) string {
	if a.kind == 's' {
		return strconv.Quote(a.val)
	}
	var ps []string
	for _, p := range a.parts {
		if p.expr.end == 0 {
			ps = append(ps, strconv.Quote(p.text))
		} else {
			ps = append(ps, markup[p.expr.start-2:p.expr.end-2])
		}
	}
	return strings.Join(ps, ", ")
}

// mergeClassUse reports whether a class attribute's expression uses the
// Class prop, and the expression it lowers to: a sum of strings with
// Class in it is ui.MergeClasses of the rest and Class; "" keeps it.
func mergeClassUse(expr, recv, ui string) (string, bool) {
	x, err := parser.ParseExpr(expr)
	if err != nil {
		return "", strings.Contains(expr, "Class")
	}
	isClass := func(n ast.Expr) bool {
		switch n := ast.Unparen(n).(type) {
		case *ast.Ident:
			return n.Name == "Class"
		case *ast.SelectorExpr:
			id, ok := n.X.(*ast.Ident)
			return ok && recv != "" && id.Name == recv && n.Sel.Name == "Class"
		}
		return false
	}
	uses := false
	ast.Inspect(x, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if isClass(sel) {
				uses = true
			}
			ast.Inspect(sel.X, func(m ast.Node) bool {
				if e, ok := m.(ast.Expr); ok && isClass(e) {
					uses = true
				}
				return true
			})
			return false
		}
		if e, ok := n.(ast.Expr); ok && isClass(e) {
			uses = true
		}
		return true
	})
	if !uses {
		return "", false
	}
	var terms []ast.Expr
	var flat func(n ast.Expr) bool
	flat = func(n ast.Expr) bool {
		if b, ok := ast.Unparen(n).(*ast.BinaryExpr); ok && b.Op == token.ADD {
			return flat(b.X) && flat(b.Y)
		}
		terms = append(terms, n)
		return true
	}
	flat(x)
	var rest []string
	class := ""
	for _, t := range terms {
		text := expr[t.Pos()-1 : t.End()-1]
		if isClass(t) && class == "" {
			class = text
			continue
		}
		rest = append(rest, text)
	}
	if len(terms) < 2 || class == "" {
		return "", true
	}
	return ui + "MergeClasses(" + strings.Join(rest, " + ") + ", " + class + ")", true
}

// hasFallthrough reports whether the JSX target at path declares
// Fallthrough; "" is the package itself.
func (e *engine) hasFallthrough(path string) bool {
	if path == "" || e.selfJSX {
		for _, f := range e.srcs {
			if !f.IsTemplate() && strings.Contains(string(f.Src), "\ntype "+fallField+" ") {
				return true
			}
		}
		return false
	}
	if e.imp == nil {
		return false
	}
	if e.fallTargets == nil {
		e.fallTargets = map[string]bool{}
	}
	if has, ok := e.fallTargets[path]; ok {
		return has
	}
	pkg, err := e.imp.Import(path)
	has := err == nil && pkg != nil && pkg.Scope().Lookup(fallField) != nil
	e.fallTargets[path] = has
	return has
}

// fallInfo is a component's Fallthrough field, and what its tag says of
// the forward element.
type fallInfo struct {
	at      int    // the field's index among the props' fields
	forward string // its tag; <X> a component's tag, ?… several roots, -… none that is an element
	own     []string
	typ     string // the component, for messages
}

// fallthroughField is the props' Fallthrough field, nil when they have
// none: a Fallthrough of the JSX target, not inside an embedded struct.
func fallthroughField(fields []propField, tg *jsxTarget, typ string) *fallInfo {
	if tg == nil {
		return nil
	}
	for i, fl := range fields {
		if fl.name != fallField || len(fl.nest) > 0 || fl.ambiguous != "" || !isNamed(fl.typ, tg.path, fallField) {
			continue
		}
		fi := &fallInfo{at: i, typ: typ, forward: "div"}
		if v, ok := vukaOption(fl.tag, "forward"); ok {
			fi.forward = v
		}
		if v, ok := vukaOption(fl.tag, "own"); ok && v != "" {
			fi.own = strings.Split(v, "|")
		}
		return fi
	}
	return nil
}

// nowhere is why the component has no element to pass its tag's
// attributes on to, "" when it has one.
func (fi *fallInfo) nowhere() (string, string) {
	switch fw := fi.forward; {
	case strings.HasPrefix(fw, "?"):
		return fi.typ + "'s markup has several roots, " + strings.ReplaceAll(fw[1:], "|", ", ") + ", and none is marked forward",
			"mark the element a caller's attributes belong on: <div forward>"
	case strings.HasPrefix(fw, "-") && len(fw) > 1:
		return fi.typ + "'s markup's root is " + fw[1:] + ", not an element",
			"wrap it in an element, or mark an element inside with forward"
	case fw == "-" || fw == "":
		return fi.typ + "'s markup has no element", "give it one"
	case strings.HasPrefix(fw, "<"):
		return fi.typ + "'s forward element is the component " + fw + ", and a component's tag passes on only to an element",
			"mark an element of " + fi.typ + "'s markup with forward, or set the attribute on " + fw + " in " + fi.typ + "'s markup"
	}
	return "", ""
}

func (fi *fallInfo) element() string { return "<" + fi.forward + ">" }

// fallthroughArg decides an attribute of c's tag no prop takes, on a
// component with a Fallthrough: the attribute it passes on, or why it
// can't, with the error's code. A caller's class merges into the forward
// element's.
func (e *engine) fallthroughArg(c *jsxComp, fi *fallInfo, a *jsxAttr, names []string) (*compArg, diag.Code, string) {
	el := c.el
	if a.pass || a.name != "class" && a.name != "className" {
		return nil, "E0106", unknownAttr(el.tag, a.name, "fields", names)
	}
	if why, help := fi.nowhere(); why != "" {
		return nil, "E0138", "<" + el.tag + " " + a.name + "> passes " + a.name + " on, and " + why + "; " + help
	}
	return &compArg{attr: a, typ: types.Universe.Lookup("any").Type()}, "", ""
}

// code is the code of the error binding c's attributes failed with: its
// own, else def.
func (c *jsxComp) code(def diag.Code) diag.Code {
	if c.failCode != "" {
		return c.failCode
	}
	return def
}
