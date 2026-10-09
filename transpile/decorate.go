package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// decoKind is the form a decorator takes, known once its type is.
type decoKind int

const (
	decoTyped decoKind = iota // func(F) F, or for a type a generic func[T]
	decoCall                  // vuka.Decorator: func(*vuka.Call), for any function
	decoType                  // func(*vuka.Type), for a type
)

// decoUse is one decorator on one declaration.
type decoUse struct {
	a       *Attr
	kind    decoKind
	decided bool
	at      int // where the copy of its name starts in f.deco, this render
}

// funcDeco is a decorated function or method: the pieces of its wrapper,
// rendered again whenever a decorator's form becomes known.
type funcDeco struct {
	uses               []*decoUse
	nameOff            int
	name, impl, state  string
	doc                string
	recv, ref          string // the wrapper's receiver clause; the body's reference
	tparams            string
	params, args       []string // the wrapper's parameters; the arguments it passes (receiver first)
	ftypes             []string // F's parameter types, receiver first
	results            string   // the results clause, with its leading space
	rtypes             []string // each result's type
	fn                 string   // F
	generic, method    bool
	init               bool
	qualName           string
	errIndex, ctxIndex int
	attrs              []*Attr // the declaration's typed attributes
}

// typeDeco is a decorated type.
type typeDeco struct {
	uses     []*decoUse
	nameOff  int
	name     string
	qualName string
	attrs    []*Attr
}

// decorate lowers every decorator, in the first round, and reports whether
// there were any. A decorator is either
//
//   - a vuka.Decorator, func(c *vuka.Call): it runs around c.Next() and works on
//     any function or method; or
//   - typed, func(F) F for the function's type F: no boxing, full types.
//
// Either way the function keeps its name for a wrapper that builds the
// decorated function once, on first call, and calls it; the body moves to
// __name. A type's decorators are func(*vuka.Type), or generic functions
// called with the type as first type argument; both run at init. Which form a
// decorator takes comes from its type, so the wrappers are rendered again once
// the types are known.
func (e *engine) decorate() bool {
	any := false
	for _, f := range e.vuka {
		groups := map[ast.Decl][]*Attr{}
		var order []ast.Decl
		for _, a := range f.attrs {
			switch {
			case a.bare && !e.namesType(f, a.Name):
				a.kind, a.Decorator, a.bare = attrDecorator, true, false
			case a.kind == attrDecorator && e.isType(f, a.Name):
				// @Perm("orders.write"): a conversion, so a typed attribute.
				a.kind, a.Decorator = attrTyped, false
			}
			if a.kind != attrDecorator {
				continue
			}
			if a.decl == nil {
				e.errs.add(a.Pos, "@%s isn't attached to a declaration", a.Name)
				continue
			}
			if groups[a.decl] == nil {
				order = append(order, a.decl)
			}
			groups[a.decl] = append(groups[a.decl], a)
		}
		for _, d := range order {
			any = true
			switch d := d.(type) {
			case *ast.FuncDecl:
				e.decorateFunc(f, d, groups[d])
			case *ast.GenDecl:
				if d.Tok == token.TYPE {
					e.decorateType(f, d, groups[d])
				} else {
					e.errs.add(groups[d][0].Pos, "decorators apply to functions, methods and types")
				}
			}
		}
		e.render(f)
	}
	return any
}

func uses(attrs []*Attr) []*decoUse {
	out := make([]*decoUse, len(attrs))
	for i, a := range attrs {
		out[i] = &decoUse{a: a}
	}
	return out
}

// typedAttrs are the typed attributes written on the same declaration as a.
func typedAttrs(f *fileState, decl ast.Decl) []*Attr {
	var out []*Attr
	for _, a := range f.attrs {
		if a.decl == decl && a.kind == attrTyped {
			out = append(out, a)
		}
	}
	return out
}

// isType reports whether name certainly names a type.
func (e *engine) isType(f *fileState, name string) bool {
	if !strings.Contains(name, ".") {
		return e.typeNames[name]
	}
	return e.namesType(f, name) && e.resolves(f, name)
}

// resolves reports whether a qualified name exists in the package it names.
func (e *engine) resolves(f *fileState, name string) bool {
	pkgName, sel, _ := strings.Cut(name, ".")
	for _, imp := range f.ast.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		pkg, err := e.imp.Import(path)
		if err != nil {
			continue
		}
		local := pkg.Name()
		if imp.Name != nil {
			local = imp.Name.Name
		}
		if local == pkgName {
			return pkg.Scope().Lookup(sel) != nil
		}
	}
	return false
}

// namesType reports whether a bare attribute names a type (so it is metadata)
// rather than a function (a decorator). A name that can't be resolved counts as
// a type, and Go reports it.
func (e *engine) namesType(f *fileState, name string) bool {
	pkgName, sel, qualified := strings.Cut(name, ".")
	if !qualified {
		return e.typeNames[name] || !e.declared[name]
	}
	for _, imp := range f.ast.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		pkg, err := e.imp.Import(path)
		if err != nil {
			continue
		}
		local := pkg.Name()
		if imp.Name != nil {
			local = imp.Name.Name
		}
		if local != pkgName {
			continue
		}
		obj := pkg.Scope().Lookup(sel)
		_, isType := obj.(*types.TypeName)
		return isType || obj == nil
	}
	return true
}

func (e *engine) decorateFunc(f *fileState, fd *ast.FuncDecl, attrs []*Attr) {
	d := &funcDeco{uses: uses(attrs), name: fd.Name.Name, nameOff: f.orig(fd.Name.Pos()),
		method: fd.Recv != nil, errIndex: -1, ctxIndex: -1, attrs: typedAttrs(f, fd)}
	if f.decorated == nil {
		f.decorated = map[int]string{}
	}

	// An overload keeps its mangled name for the wrapper; its body moves on.
	if set := e.sets[declKey(fd)]; set != nil {
		for _, o := range set.list {
			if o.nameOff == d.nameOff {
				d.name = o.mangled
			}
		}
		for i, ed := range f.fixed {
			if ed.start == d.nameOff {
				f.fixed = append(f.fixed[:i], f.fixed[i+1:]...)
				break
			}
		}
	}
	d.impl = "__" + d.name
	f.add(d.nameOff, d.nameOff+len(fd.Name.Name), d.impl)
	f.decorated[attrs[0].declOff] = d.name
	d.qualName = e.pkgName() + "." + fd.Name.Name
	if d.method {
		d.qualName = recvBase(fd) + "." + fd.Name.Name
	}
	f.decos = append(f.decos, d)
	if !d.method && d.name == "init" {
		d.init = true
		return
	}

	named := true
	for _, field := range fd.Type.Params.List {
		if len(field.Names) == 0 {
			named = false
		}
		for _, n := range field.Names {
			if n.Name == "_" {
				named = false
			}
		}
	}
	for _, field := range fd.Type.Params.List {
		t := e.qualText(f, field.Type)
		_, variadic := field.Type.(*ast.Ellipsis)
		for k := range max(1, len(field.Names)) {
			p := "a" + itoa(len(d.params))
			if named {
				p = field.Names[k].Name
			}
			if t == "context.Context" && d.ctxIndex < 0 {
				d.ctxIndex = len(d.params)
			}
			d.ftypes = append(d.ftypes, t)
			d.params = append(d.params, p+" "+t)
			if variadic {
				p += "..."
			}
			d.args = append(d.args, p)
		}
	}
	if res := fd.Type.Results; res != nil {
		d.results = " " + e.qualText(f, res)
		for _, field := range res.List {
			t := e.qualText(f, field.Type)
			for range max(1, len(field.Names)) {
				d.rtypes = append(d.rtypes, t)
			}
		}
		if n := len(d.rtypes); n > 0 && d.rtypes[n-1] == "error" {
			d.errIndex = n - 1
		}
	}

	// A generic function's wrapper instantiates the body with its own type
	// parameters, and caches one decorated function per instantiation.
	targs := ""
	if tp := fd.Type.TypeParams; tp != nil {
		d.tparams = e.qualText(f, tp)
		var names []string
		for _, field := range tp.List {
			for _, n := range field.Names {
				names = append(names, n.Name)
			}
		}
		targs = "[" + strings.Join(names, ", ") + "]"
	}
	d.generic = d.tparams != "" || d.method && genericRecv(fd)

	d.ref, d.state = d.impl+targs, "__"+d.name+"_dec"
	if d.method {
		field := fd.Recv.List[0]
		rt := e.qualText(f, field.Type)
		rn := "r"
		if len(field.Names) > 0 && field.Names[0].Name != "_" {
			rn = field.Names[0].Name
		}
		d.recv = "(" + rn + " " + rt + ") "
		d.ftypes = append([]string{rt}, d.ftypes...)
		d.args = append([]string{rn}, d.args...)
		d.ref = "(" + rt + ")." + d.impl
		d.state = "__" + recvBase(fd) + "_" + d.name + "_dec"
	}
	d.fn = "func(" + strings.Join(d.ftypes, ", ") + ")" + d.results
	if fd.Doc != nil {
		d.doc = trimDoc(f.nodeText(fd.Doc))
	}
}

// decorateType runs a type's decorators at init. On a group, they apply to
// every type in it.
func (e *engine) decorateType(f *fileState, gd *ast.GenDecl, attrs []*Attr) {
	for _, spec := range gd.Specs {
		ts := spec.(*ast.TypeSpec)
		if ts.TypeParams != nil {
			e.errs.add(attrs[0].Pos, "%s is generic; a type decorator needs a type it can name, so instantiate it in a decorated alias: type IntBox = Box[int]", ts.Name.Name)
			continue
		}
		f.decos = append(f.decos, &typeDeco{uses: uses(attrs), nameOff: f.orig(ts.Name.Pos()),
			name: ts.Name.Name, qualName: e.pkgName() + "." + ts.Name.Name, attrs: typedAttrs(f, gd)})
	}
}

func (e *engine) pkgName() string {
	for _, f := range e.files {
		if f.ast != nil {
			return f.ast.Name.Name
		}
	}
	return ""
}

// render writes f's wrappers for the decorators' current forms.
func (e *engine) render(f *fileState) {
	f.deco = genWriter{}
	for _, d := range f.decos {
		switch d := d.(type) {
		case *funcDeco:
			e.renderFunc(f, d)
		case *typeDeco:
			e.renderType(f, d)
		}
	}
	f.makeTrailer(e.bare)
}

func (e *engine) renderFunc(f *fileState, d *funcDeco) {
	w, rt := &f.deco, f.runtime()
	if d.init {
		w.gen("\n", d.nameOff)
		if !e.bare {
			w.gen(lineDirective(f.at(d.nameOff)), d.nameOff)
		}
		w.gen("func init() {\n\t", d.nameOff)
		for _, u := range d.uses {
			if u.kind == decoCall {
				e.errs.add(u.a.Pos, "init takes no arguments and returns nothing; decorate it with a func(func()) func()")
			}
			e.writeDecorator(f, u, "")
			w.gen("(", u.a.end)
		}
		w.gen(d.impl+strings.Repeat(")", len(d.uses))+"()\n}\n", d.nameOff)
		return
	}

	w.gen("\n", d.nameOff)
	if d.doc != "" {
		w.gen(d.doc+"\n", d.nameOff)
	}
	if d.generic {
		w.gen("var "+d.state+" "+rt+".Instances\n\n", d.nameOff)
	} else {
		w.gen("var "+d.state+" "+rt+".Decorated["+d.fn+"]\n\n", d.nameOff)
	}
	info := d.state + "_info"
	hasCall := false
	for _, u := range d.uses {
		hasCall = hasCall || u.kind == decoCall
	}
	if hasCall {
		zeros := make([]string, len(d.rtypes))
		for i, t := range d.rtypes {
			zeros[i] = "*new(" + t + ")"
		}
		w.gen("var "+info+" = "+rt+".Func{Name: "+strconv.Quote(d.qualName)+", ErrIndex: "+itoa(d.errIndex)+
			", CtxIndex: "+itoa(d.ctxIndex)+", Zero: func() []any { return []any{"+strings.Join(zeros, ", ")+"} }, Attrs: []any{", d.nameOff)
		e.writeAttrValues(f, d.attrs)
		w.gen("}}\n\n", d.nameOff)
	}
	if !e.bare {
		w.gen(lineDirective(f.at(d.nameOff)), d.nameOff)
	}
	w.gen("func "+d.recv+d.name+d.tparams+"("+strings.Join(d.params, ", ")+")"+d.results+" {\n\t", d.nameOff)
	if d.results != "" {
		w.gen("return ", d.nameOff)
	}
	if d.generic {
		w.gen(rt+".Instance(&"+d.state+", func() "+d.fn+" {\n\t\treturn ", d.nameOff)
	} else {
		w.gen(d.state+".Get(func() "+d.fn+" {\n\t\treturn ", d.nameOff)
	}

	// The chain, outermost first: a typed decorator applies to the function
	// value; each run of vuka.Decorators becomes one adapter.
	closers := ""
	for i := 0; i < len(d.uses); {
		u := d.uses[i]
		if u.kind != decoCall {
			e.writeDecorator(f, u, "")
			w.gen("(", u.a.end)
			closers += ")"
			i++
			continue
		}
		j := i
		for j < len(d.uses) && d.uses[j].kind == decoCall {
			j++
		}
		w.gen("func(next "+d.fn+") "+d.fn+" {\n\t\t\tchain := []"+rt+".Decorator{", u.a.start)
		for k := i; k < j; k++ {
			if k > i {
				w.gen(", ", d.uses[k].a.start)
			}
			e.writeDecorator(f, d.uses[k], "")
		}
		w.gen("}\n\t\t\treturn "+e.adapter(f, d, info)+"\n\t\t}(", u.a.start)
		closers += ")"
		i = j
	}
	w.gen(d.ref+closers+"\n\t})("+strings.Join(d.args, ", ")+")\n}\n", d.nameOff)
}

// adapter is the function that packs a call of next into a *vuka.Call, runs
// the chain, and unpacks the results.
func (e *engine) adapter(f *fileState, d *funcDeco, info string) string {
	rt := f.runtime()
	var params, args, inner []string
	recv := "nil"
	for i, t := range d.ftypes {
		p := "p" + itoa(i)
		pt, at := t, t
		if strings.HasPrefix(t, "...") {
			at = "[]" + t[3:]
		}
		params = append(params, p+" "+pt)
		if d.method && i == 0 {
			recv = p
			inner = append(inner, rt+".As["+at+"](c.Receiver)")
			continue
		}
		n := len(args)
		args = append(args, p)
		in := rt + ".As[" + at + "](c.Args[" + itoa(n) + "])"
		if strings.HasPrefix(t, "...") {
			in += "..."
		}
		inner = append(inner, in)
	}
	var rs, outs []string
	for i, t := range d.rtypes {
		rs = append(rs, "r"+itoa(i))
		outs = append(outs, rt+".As["+t+"](c.Results["+itoa(i)+"])")
	}
	invoke := "next(" + strings.Join(inner, ", ") + "); return nil"
	if len(rs) > 0 {
		invoke = strings.Join(rs, ", ") + " := next(" + strings.Join(inner, ", ") + "); return []any{" + strings.Join(rs, ", ") + "}"
	}
	body := "c := " + rt + ".NewCall(&" + info + ", chain, " + recv + ", []any{" + strings.Join(args, ", ") +
		"}, func(c *" + rt + ".Call) []any { " + invoke + " }); c.Run()"
	if len(outs) > 0 {
		body += "; return " + strings.Join(outs, ", ")
	}
	return "func(" + strings.Join(params, ", ") + ")" + d.results + " { " + body + " }"
}

func (e *engine) renderType(f *fileState, d *typeDeco) {
	w, rt := &f.deco, f.runtime()
	w.gen("\nfunc init() {\n", d.nameOff)
	desc := ""
	for _, u := range d.uses {
		w.gen("\t", u.a.start)
		if u.kind == decoType {
			if desc == "" {
				desc = "__" + d.name + "_type"
				w.gen(desc+" := "+rt+".TypeOf["+d.name+"]("+strconv.Quote(d.qualName), u.a.start)
				for _, a := range d.attrs {
					w.gen(", ", a.start)
					e.writeAttrValues(f, []*Attr{a})
				}
				w.gen(")\n\t", u.a.start)
			}
			e.writeDecorator(f, u, "")
			if !strings.HasSuffix(strings.TrimSpace(u.a.Args), ")") && u.a.Args != "" {
				w.gen("()", u.a.end)
			}
			w.gen("("+desc+")\n", u.a.end)
			continue
		}
		e.writeDecorator(f, u, d.name)
		w.gen("\n", u.a.end)
	}
	w.gen("}\n", d.nameOff)
}

// writeAttrValues writes typed attributes as values: T{…} as written, a bare
// @T as T's zero value.
func (e *engine) writeAttrValues(f *fileState, attrs []*Attr) {
	w := &f.deco
	for i, a := range attrs {
		if i > 0 {
			w.gen(", ", a.start)
		}
		text := string(f.src[a.nameStart:a.end])
		if strings.HasSuffix(text, "}") || strings.HasSuffix(text, ")") {
			w.copy(text, a.nameStart)
		} else {
			w.gen("*new(", a.start)
			w.copy(text, a.nameStart)
			w.gen(")", a.end)
		}
	}
}

// writeDecorator copies a decorator's text into f's wrappers, recording where
// its name lands. With typeArg set (a generic type decorator) the type goes in
// as the first type argument, and a bare decorator is called with no arguments.
func (e *engine) writeDecorator(f *fileState, u *decoUse, typeArg string) {
	w, a := &f.deco, u.a
	if !e.bare {
		p := a.Pos
		p.Column++
		w.gen(lineDirective(p), a.nameStart)
	}
	nameEnd := a.nameStart + len(a.Name)
	u.at = w.len()
	w.copy(string(f.src[a.nameStart:nameEnd]), a.nameStart)
	args := string(f.src[nameEnd:a.end])
	switch {
	case typeArg == "":
		w.copy(args, nameEnd)
	case strings.HasPrefix(args, "["):
		w.gen("["+typeArg+", ", nameEnd)
		w.copy(args[1:], nameEnd+1)
	default:
		w.gen("["+typeArg+"]", nameEnd)
		w.copy(args, nameEnd)
	}
	if typeArg != "" && !strings.HasSuffix(strings.TrimSpace(args), ")") {
		w.gen("()", a.end)
	}
}

// classify finds each decorator's form from its type, and renders again when
// any changed. A decorator whose type isn't known yet keeps its form.
func (e *engine) classify(f *fileState) {
	if len(f.decos) == 0 {
		return
	}
	changed := false
	for _, d := range f.decos {
		var us []*decoUse
		isType := false
		switch d := d.(type) {
		case *funcDeco:
			us = d.uses
		case *typeDeco:
			us, isType = d.uses, true
		}
		for _, u := range us {
			if u.decided {
				continue
			}
			t := e.decoratorType(f, u)
			if t == nil {
				continue
			}
			u.decided = true
			kind := decoTyped
			switch {
			case !isType && isRuntimeFunc(t, "Call"):
				kind = decoCall
			case isType && isRuntimeFunc(t, "Type"):
				kind = decoType
			}
			if kind != u.kind {
				u.kind, changed = kind, true
			}
		}
	}
	if changed {
		e.render(f)
		e.progress = true
	}
}

// decoratorType is the type of the value a decorator denotes: its name's type,
// or for @name(args) what calling it returns.
func (e *engine) decoratorType(f *fileState, u *decoUse) types.Type {
	start := f.body + f.decoBase + u.at
	end := start + len(u.a.Name)
	var obj types.Object
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if n == nil || obj != nil || f.off(n.End()) < start || f.off(n.Pos()) > end {
			return false
		}
		if f.off(n.Pos()) == start && f.off(n.End()) == end {
			switch x := n.(type) {
			case *ast.Ident:
				obj = e.info.Uses[x]
			case *ast.SelectorExpr:
				obj = e.info.Uses[x.Sel]
			}
		}
		return true
	})
	if obj == nil || isInvalid(obj.Type()) {
		return nil
	}
	t := obj.Type()
	if strings.HasPrefix(strings.TrimSpace(u.a.Args), "(") {
		sig, ok := t.Underlying().(*types.Signature)
		if !ok || sig.Results().Len() != 1 {
			return t
		}
		return sig.Results().At(0).Type()
	}
	return t
}

// isRuntimeFunc reports whether t is func(*vuka.<name>) with no results.
func isRuntimeFunc(t types.Type, name string) bool {
	sig, ok := t.Underlying().(*types.Signature)
	if !ok || sig.Params().Len() != 1 || sig.Results().Len() != 0 {
		return false
	}
	p, ok := sig.Params().At(0).Type().(*types.Pointer)
	if !ok {
		return false
	}
	n, ok := p.Elem().(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == RuntimePath && n.Obj().Name() == name
}

// trimDoc drops the empty comment lines attributes leave around a doc comment.
func trimDoc(doc string) string {
	lines := strings.Split(doc, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "//" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "//" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

func genericRecv(fd *ast.FuncDecl) bool {
	t := fd.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	switch t.(type) {
	case *ast.IndexExpr, *ast.IndexListExpr:
		return true
	}
	return false
}

// qualText is a node's source text with Result, Option and friends pointed at
// the runtime, for code Vuka writes outside the source's own lines.
func (e *engine) qualText(f *fileState, n ast.Node) string {
	unresolved := map[*ast.Ident]bool{}
	for _, id := range f.ast.Unresolved {
		unresolved[id] = true
	}
	start, end := f.off(n.Pos()), f.off(n.End())
	var b strings.Builder
	last := start
	ast.Inspect(n, func(x ast.Node) bool {
		id, ok := x.(*ast.Ident)
		if ok && unresolved[id] && runtimeNames[id.Name] && !e.declared[id.Name] && !f.dotImps {
			o := f.off(id.Pos())
			b.Write(f.text[last:o])
			b.WriteString(f.runtime() + ".")
			last = o
		}
		return true
	})
	b.Write(f.text[last:end])
	return b.String()
}
