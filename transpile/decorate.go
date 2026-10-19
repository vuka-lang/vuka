package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strconv"
	"strings"
)

// decoKind is the form a decorator takes, known once its type is.
type decoKind int

const (
	decoTyped    decoKind = iota // func(F) F, or for a type a generic func[T]
	decoCall                     // vuka.Decorator: func(*vuka.Call), for any function
	decoType                     // func(*vuka.Type), for a type
	decoDecl                     // func(*vuka.Decl), a declarer: runs once at init, for a function
	decoBundle                   // vuka.Bundle: decorators and attributes composed into one
	decoRegistry                 // a method of its package's registry: a call at init with the declaration
	decoOption                   // an option of a registry decorator on the same declaration
)

// registryUse is how a registry decorator is called: @web.Get("/x") on
// ShowPet is web.Routes.Get("/x", ShowPet, options…).
type registryUse struct {
	field  string     // the registry variable ("Routes")
	before int        // the method's parameters before the declaration's
	elem   types.Type // its options' type; nil when it takes none
	params bool       // the package has Params(...string), an option naming parameters
}

// decoUse is one decorator on one declaration.
type decoUse struct {
	a       *Attr
	kind    decoKind
	decided bool
	called  bool         // written bare, but a function of optional arguments only: called with none
	at      int          // where the copy of its name starts in f.deco, this render
	bvar    string       // a bundle's value, this render
	advice  string       // set instead of a: the function giving a type's advice, for one of its methods
	t       types.Type   // the decorator's value's type, once decided
	reg     *registryUse // a registry decorator's call
	twin    string       // a type decorator's Register<Name>[T] in its package, called in its place
}

// funcDeco is a decorated function or method: the pieces of its wrapper,
// rendered again whenever a decorator's form becomes known.
type funcDeco struct {
	uses               []*decoUse
	nameOff            int
	name, impl, state  string
	doc                string
	recv, ref          string // the wrapper's receiver clause; the body's reference
	rname, rtype       string // a method's receiver
	tparams            string
	params, args       []string // the wrapper's parameters; the arguments it passes (receiver first)
	pnames             []string // the parameters' names as written, "" when unnamed
	ftypes             []string // F's parameter types, receiver first
	results            string   // the results clause, with its leading space
	rtypes             []string // each result's type
	fn                 string   // F
	generic, method    bool
	init               bool
	qualName           string
	errIndex, ctxIndex int
	attrs              []*Attr   // the declaration's typed attributes
	pattrs             [][]*Attr // each parameter's attributes, receiver excluded; nil when none has any
	advice             []string  // the functions giving its type's advice, for a method
	file               string    // the source file's base name
	line               int       // the name's line
}

// typeDeco is a decorated type.
type typeDeco struct {
	uses     []*decoUse
	nameOff  int
	name     string
	qualName string
	attrs    []*Attr
	advised  bool // its call decorators were given to its methods

	// For a struct: its constructor, the static New. It is the type's own
	// when it declares one; otherwise Vuka writes one taking each injected
	// field as a parameter.
	isStruct   bool
	hasNew     bool
	ctorParams []string // "db *DB"
	ctorInits  []string // "db: db"
}

// injected reports whether a struct field is a dependency its constructor
// takes: every named field but _ and those tagged inject:"-"; an embedded
// field only when it is a pointer or interface (embedding a value is
// composition, not a dependency).
func injected(field *ast.Field, name string, embedded bool) bool {
	if name == "_" {
		return false
	}
	if field.Tag != nil {
		if tag, err := strconv.Unquote(field.Tag.Value); err == nil && reflectTag(tag, "inject") == "-" {
			return false
		}
	}
	if embedded {
		switch field.Type.(type) {
		case *ast.StarExpr, *ast.InterfaceType:
			return true
		}
		return false
	}
	return true
}

// reflectTag is reflect.StructTag.Get, without importing reflect here.
func reflectTag(tag, key string) string {
	for tag != "" {
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		i = 0
		for i < len(tag) && tag[i] > ' ' && tag[i] != ':' && tag[i] != '"' {
			i++
		}
		if i == 0 || i+1 >= len(tag) || tag[i] != ':' || tag[i+1] != '"' {
			return ""
		}
		name := tag[:i]
		tag = tag[i+1:]
		i = 1
		for i < len(tag) && tag[i] != '"' {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			return ""
		}
		value := tag[:i+1]
		tag = tag[i+1:]
		if name == key {
			v, err := strconv.Unquote(value)
			if err != nil {
				return ""
			}
			return v
		}
	}
	return ""
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
	e.prepareBundles()
	for _, f := range e.vuka {
		any = any || len(f.bundles) > 0
		groups := map[ast.Decl][]*Attr{}
		var order []ast.Decl
		for _, a := range f.attrs {
			if f.rt == "" && strings.HasPrefix(a.Name, "vuka.") && e.isRuntimeName(f, a.Name, a.Name[len("vuka."):]) {
				f.runtime() // @vuka.Targets(…), @vuka.NoAdvice: imported when the file doesn't
				any = true
			}
			if a.Param {
				if a.decl == nil {
					e.errs.add("E0205", a.Pos, "@%s must be followed by a parameter of a top-level function", a.Name)
				} else if a.bare && !e.namesType(f, a.Name) {
					e.errs.add("E0205", a.Pos, "@%s is a function, not a value; a parameter attribute is a value, such as @Body or @Path(\"id\")", a.Name)
				}
				continue
			}
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
				e.errs.add("E0204", a.Pos, "@%s isn't attached to a declaration", a.Name)
				continue
			}
			if groups[a.decl] == nil {
				order = append(order, a.decl)
			}
			groups[a.decl] = append(groups[a.decl], a)
		}
		if len(order) > 0 {
			// Imported until the decorators' forms are known: a decorator's
			// own declaration may name vuka.Type; unusedRuntime drops it
			// when nothing does.
			f.runtime()
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
					e.errs.add("E0207", groups[d][0].Pos, "decorators apply to functions, methods and types")
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

// typedAttrs are the typed attributes written on decl.
func typedAttrs(f *fileState, decl ast.Decl) []*Attr {
	var out []*Attr
	for _, a := range f.attrs {
		if a.decl == decl && a.kind == attrTyped {
			out = append(out, a)
		}
	}
	return out
}

// typedAttrsAt are the typed attributes written on the declaration whose
// keyword is at src offset off.
func typedAttrsAt(f *fileState, off int) []*Attr {
	var out []*Attr
	for _, a := range f.attrs {
		if a.declOff == off && a.kind == attrTyped && !a.Field && !a.Param {
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
	pkg := e.importedAs(f, pkgName)
	return pkg != nil && pkg.Scope().Lookup(sel) != nil
}

// importedAs is the package f imports as name; the runtime is vuka even
// before Vuka adds its import.
func (e *engine) importedAs(f *fileState, name string) *types.Package {
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
		if local == name {
			return pkg
		}
	}
	if name == "vuka" {
		if pkg, err := e.imp.Import(RuntimePath); err == nil {
			return pkg
		}
	}
	return nil
}

// namesType reports whether a bare attribute names a type (so it is metadata)
// rather than a function (a decorator). A name that can't be resolved counts as
// a type, and Go reports it.
func (e *engine) namesType(f *fileState, name string) bool {
	pkgName, sel, qualified := strings.Cut(name, ".")
	if !qualified {
		return e.typeNames[name] || !e.declared[name]
	}
	pkg := e.importedAs(f, pkgName)
	if pkg == nil {
		return true
	}
	obj := pkg.Scope().Lookup(sel)
	_, isType := obj.(*types.TypeName)
	return isType || obj == nil
}

func (e *engine) decorateFunc(f *fileState, fd *ast.FuncDecl, attrs []*Attr) *funcDeco {
	nameOff, declOff := f.orig(fd.Name.Pos()), f.orig(fd.Type.Func)
	name := srcIdent(f.src, nameOff) // as written: a later round's tree has an overload's mangled name
	d := &funcDeco{uses: uses(attrs), name: name, nameOff: nameOff,
		method: fd.Recv != nil && len(fd.Recv.List) > 0, errIndex: -1, ctxIndex: -1, attrs: typedAttrsAt(f, declOff)}
	d.file, d.line = filepath.Base(f.name), f.at(d.nameOff).Line
	if f.decorated == nil {
		f.decorated = map[int]string{}
	}
	key := name
	if d.method {
		key = recvBase(fd) + "." + name
	}

	// An overload keeps its mangled name for the wrapper; its body moves on.
	if set := e.sets[key]; set != nil {
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
	f.add(d.nameOff, d.nameOff+len(name), d.impl)
	f.decorated[declOff] = d.name
	d.qualName = e.pkgName() + "." + name
	if d.method {
		d.qualName = key
	}
	f.decos = append(f.decos, d)
	if !d.method && d.name == "init" {
		d.init = true
		return d
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
			pname := ""
			if len(field.Names) > 0 {
				pname = field.Names[k].Name
			}
			d.pnames = append(d.pnames, pname)
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
	for _, a := range f.attrs {
		if a.Param && a.declOff == declOff {
			if d.pattrs == nil {
				d.pattrs = make([][]*Attr, len(d.pnames))
			}
			d.pattrs[a.ParamIndex] = append(d.pattrs[a.ParamIndex], a)
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
		// An unnamed receiver gets a name no parameter, result or type in
		// the signature can be: "r" would clash with a parameter r.
		rn := "__recv"
		if len(field.Names) > 0 && field.Names[0].Name != "_" {
			rn = field.Names[0].Name
		}
		d.rname, d.rtype = rn, rt
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
	return d
}

// srcIdent is the identifier at off in src.
func srcIdent(src []byte, off int) string {
	end := off
	for end < len(src) && (src[end] == '_' || src[end] >= '0' && src[end] <= '9' || src[end] >= 'a' && src[end] <= 'z' || src[end] >= 'A' && src[end] <= 'Z' || src[end] >= 0x80) {
		end++
	}
	return string(src[off:end])
}

// decorateType runs a type's decorators at init. On a group, they apply to
// every type in it.
func (e *engine) decorateType(f *fileState, gd *ast.GenDecl, attrs []*Attr) {
	for _, spec := range gd.Specs {
		ts := spec.(*ast.TypeSpec)
		if ts.TypeParams != nil {
			e.errs.add("E0211", attrs[0].Pos, "%s is generic; a type decorator needs a type it can name, so instantiate it in a decorated alias: type IntBox = Box[int]", ts.Name.Name)
			continue
		}
		d := &typeDeco{uses: uses(attrs), nameOff: f.orig(ts.Name.Pos()),
			name: ts.Name.Name, qualName: e.pkgName() + "." + ts.Name.Name, attrs: typedAttrs(f, gd),
			hasNew: e.declared[staticGoName(ts.Name.Name, "New")]}
		if st, ok := ts.Type.(*ast.StructType); ok {
			d.isStruct = true
			for _, field := range st.Fields.List {
				t := e.qualText(f, field.Type)
				names := field.Names
				if len(names) == 0 {
					names = []*ast.Ident{ast.NewIdent(embeddedName(field.Type))}
				}
				for _, n := range names {
					if injected(field, n.Name, len(field.Names) == 0) {
						d.ctorParams = append(d.ctorParams, n.Name+" "+t)
						d.ctorInits = append(d.ctorInits, n.Name+": "+n.Name)
					}
				}
			}
		}
		f.decos = append(f.decos, d)
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
	f.deco, f.regs = genWriter{}, nil
	e.renderFiles(f)
	e.renderFields(f)
	e.renderTargets(f)
	for _, d := range f.decos {
		switch d := d.(type) {
		case *funcDeco:
			nameBundles(d.uses, d.state)
		case *typeDeco:
			nameBundles(d.uses, "__"+d.name)
		}
	}
	e.renderRegistry(f)
	for _, d := range f.decos {
		switch d := d.(type) {
		case *funcDeco:
			e.renderFunc(f, d)
		case *typeDeco:
			e.renderType(f, d)
		}
	}
	e.renderRegistrations(f)
	f.makeTrailer(e.bare)
}

func (e *engine) renderFunc(f *fileState, d *funcDeco) {
	w := &f.deco
	if d.init {
		w.gen("\n", d.nameOff)
		if !e.bare {
			w.gen(lineDirective(f.at(d.nameOff)), d.nameOff)
		}
		w.gen("func init() {\n\t", d.nameOff)
		for _, u := range d.uses {
			if u.kind == decoCall || u.kind == decoBundle {
				e.errs.add("E0212", u.a.Pos, "init takes no arguments and returns nothing; decorate it with a func(func()) func()")
			}
			e.writeDecorator(f, u, "")
			w.gen("(", u.a.end)
		}
		w.gen(d.impl+strings.Repeat(")", len(d.uses))+"()\n}\n", d.nameOff)
		return
	}

	// The type's advice runs outside the function's own decorators.
	var wraps, decls []*decoUse
	for _, a := range d.advice {
		wraps = append(wraps, &decoUse{kind: decoCall, advice: a})
	}
	for _, u := range d.uses {
		switch u.kind {
		case decoDecl:
			decls = append(decls, u)
		case decoBundle:
			decls = append(decls, u)
			wraps = append(wraps, u)
		case decoRegistry:
			fn := d.name
			if d.method {
				fn = "(" + d.rtype + ")." + d.name
			}
			f.regs = append(f.regs, registration{u: u, decl: fn, uses: d.uses, params: e.paramNames(d)})
		case decoOption:
		default:
			wraps = append(wraps, u)
		}
	}
	e.keepName(f, d, len(wraps) == 0)
	if len(wraps) == 0 {
		// Declarers alone: the function stays as it is.
		e.renderDecls(f, d, decls)
		return
	}
	rt := f.runtime()
	w.gen("\n", d.nameOff)
	doc := func() {
		if d.doc != "" {
			w.gen(d.doc+"\n", d.nameOff)
		}
	}
	e.writeBundleVars(f, d.uses, d.state)
	if d.generic {
		w.gen("var "+d.state+" "+rt+".Instances\n\n", d.nameOff)
	} else {
		w.gen("var "+d.state+" "+rt+".Decorated["+d.fn+"]\n\n", d.nameOff)
	}
	info := d.state + "_info"
	hasCall := false
	for _, u := range wraps {
		hasCall = hasCall || isCallLike(u.kind)
	}
	if hasCall {
		zeros := make([]string, len(d.rtypes))
		for i, t := range d.rtypes {
			zeros[i] = "*new(" + t + ")"
		}
		w.gen("var "+info+" = "+rt+".Func{Name: "+strconv.Quote(d.qualName)+", ErrIndex: "+itoa(d.errIndex)+
			", CtxIndex: "+itoa(d.ctxIndex)+", Zero: func() []any { return []any{"+strings.Join(zeros, ", ")+"} }, Attrs: ", d.nameOff)
		e.writeAttrsWith(f, d.attrs, d.uses, d.nameOff)
		if d.pattrs != nil {
			w.gen(", ParamAttrs: [][]any{", d.nameOff)
			for i, as := range d.pattrs {
				if i > 0 {
					w.gen(", ", d.nameOff)
				}
				e.writeParamAttrs(f, as, "nil", d.nameOff)
			}
			w.gen("}", d.nameOff)
		}
		w.gen("}\n\n", d.nameOff)
	}
	doc()
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
	for i := 0; i < len(wraps); {
		u := wraps[i]
		if !isCallLike(u.kind) {
			e.writeDecorator(f, u, "")
			w.gen("(", u.a.end)
			closers += ")"
			i++
			continue
		}
		j := i
		for j < len(wraps) && isCallLike(wraps[j].kind) {
			j++
		}
		w.gen("func(next "+d.fn+") "+d.fn+" {\n\t\t\tchain := ", d.nameOff)
		dynamic := e.writeChain(f, wraps[i:j], d.nameOff, func(u *decoUse) { w.gen(u.bvar, u.a.start) })
		w.gen("\n\t\t\t", d.nameOff)
		if dynamic {
			// A bundle may hold no call decorator: then nothing wraps next.
			w.gen("if len(chain) == 0 {\n\t\t\t\treturn next\n\t\t\t}\n\t\t\t", d.nameOff)
		}
		w.gen("return "+e.adapter(f, d, info)+"\n\t\t}(", d.nameOff)
		closers += ")"
		i = j
	}
	w.gen(d.ref+closers+"\n\t})("+strings.Join(d.args, ", ")+")\n}\n", d.nameOff)
	e.renderDecls(f, d, decls)
}

func isCallLike(k decoKind) bool { return k == decoCall || k == decoBundle }

// writeChain writes a run of call decorators as a []vuka.Decorator: the
// type's advice and bundles spliced in, bundle writing a bundle's value. It
// reports whether the run's length is only known at run time.
func (e *engine) writeChain(f *fileState, run []*decoUse, off int, bundle func(u *decoUse)) bool {
	w, rt := &f.deco, f.runtime()
	lead := 0
	for lead < len(run) && run[lead].kind == decoCall && run[lead].advice == "" {
		lead++
	}
	rest := run[lead:]
	if lead == 0 && len(rest) > 0 && rest[0].advice != "" {
		w.gen(strings.Repeat("append(", len(rest)-1)+rest[0].advice+"()", off)
		rest = rest[1:]
	} else {
		w.gen(strings.Repeat("append(", len(rest))+"[]"+rt+".Decorator{", off)
		for k, u := range run[:lead] {
			if k > 0 {
				w.gen(", ", u.a.start)
			}
			e.writeDecorator(f, u, "")
		}
		w.gen("}", off)
	}
	for _, u := range rest {
		w.gen(", ", off)
		switch {
		case u.advice != "":
			w.gen(u.advice+"()...", off)
		case u.kind == decoBundle:
			bundle(u)
			w.gen(".Calls()...", off)
		default:
			e.writeDecorator(f, u, "")
		}
		w.gen(")", off)
	}
	return len(run) > lead
}

// writeBundleVars declares the value of each bundle among uses, once per
// decorated declaration.
func (e *engine) writeBundleVars(f *fileState, uses []*decoUse, state string) {
	w := &f.deco
	for _, u := range uses {
		if u.kind != decoBundle {
			continue
		}
		w.gen("var "+u.bvar+" = ", u.a.start)
		e.writeDecorator(f, u, "")
		w.gen("\n\n", u.a.end)
	}
}

// nameBundles names the value of each bundle among uses, before the
// registry's init, which reads their attributes, is written.
func nameBundles(uses []*decoUse, state string) {
	n := 0
	for _, u := range uses {
		if u.kind == decoBundle {
			u.bvar = state + "_b" + itoa(n)
			n++
		}
	}
}

// writeAttrsWith writes a declaration's typed attributes as a []any, with
// those of its bundles after them.
func (e *engine) writeAttrsWith(f *fileState, attrs []*Attr, uses []*decoUse, off int) {
	w := &f.deco
	var bvars []string
	for _, u := range uses {
		if u.kind == decoBundle {
			bvars = append(bvars, u.bvar)
		}
	}
	w.gen(strings.Repeat("append(", len(bvars))+"[]any{", off)
	e.writeAttrValues(f, attrs)
	w.gen("}", off)
	for _, b := range bvars {
		w.gen(", "+b+".Attrs()...)", off)
	}
}

// renderDecls runs a function's declarers at init, top to bottom, with a
// description of the decorated function: vuka.DeclOf, its attributes those
// renderRegistry registered.
func (e *engine) renderDecls(f *fileState, d *funcDeco, decls []*decoUse) {
	if len(decls) == 0 || d.generic {
		return
	}
	w, rt := &f.deco, f.runtime()
	fn := d.name
	if d.method {
		fn = "(" + d.rtype + ")." + d.name
	}
	desc := func() {
		w.gen(rt+".DeclOf("+fn, d.nameOff)
		named := d.pattrs != nil
		for _, n := range d.pnames {
			named = named || n != ""
		}
		for i, n := range d.pnames {
			if !named {
				break
			}
			w.gen(", "+rt+".Param{Name: "+strconv.Quote(n), d.nameOff)
			if d.pattrs != nil && len(d.pattrs[i]) > 0 {
				w.gen(", Attrs: ", d.nameOff)
				e.writeParamAttrs(f, d.pattrs[i], "", d.nameOff)
			}
			w.gen("}", d.nameOff)
		}
		w.gen(")", d.nameOff)
	}
	w.gen("\nfunc init() {\n\t", d.nameOff)
	if len(decls) == 1 {
		u := decls[0]
		if u.kind == decoBundle {
			w.gen(u.bvar+".Declare(", u.a.start)
		} else {
			e.writeDecorator(f, u, "")
			w.gen("(", u.a.end)
		}
		desc()
		w.gen(")\n}\n", d.nameOff)
		return
	}
	v := d.state + "l"
	w.gen(v+" := ", d.nameOff)
	desc()
	w.gen("\n", d.nameOff)
	for _, u := range decls {
		w.gen("\t", u.a.start)
		if u.kind == decoBundle {
			w.gen(u.bvar+".Declare("+v+")\n", u.a.start)
			continue
		}
		e.writeDecorator(f, u, "")
		w.gen("("+v+")\n", u.a.end)
	}
	w.gen("}\n", d.nameOff)
}

// keepName gives a function its name back when nothing wraps it: its
// declarers and registry decorators take it as it is.
func (e *engine) keepName(f *fileState, d *funcDeco, keep bool) {
	text := d.impl
	if keep {
		text = d.name
	}
	for i, ed := range f.fixed {
		if ed.start == d.nameOff && (ed.text == d.impl || ed.text == d.name) && ed.end > ed.start {
			f.fixed[i].text = text
			return
		}
	}
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
	w := &f.deco
	var advice, inits []*decoUse
	described, bundled, registered, ndesc := false, false, false, 0
	for _, u := range d.uses {
		switch u.kind {
		case decoCall:
			advice = append(advice, u)
			continue
		case decoOption:
			continue
		case decoRegistry:
			registered = true
			continue
		case decoBundle:
			advice = append(advice, u)
			bundled = true
			fallthrough
		case decoType:
			if u.twin != "" {
				break
			}
			described = true
			ndesc++
		}
		inits = append(inits, u)
	}
	ctor := "nil"
	if (described || registered) && d.isStruct {
		ctor = staticGoName(d.name, "New")
	}
	if ctor != "nil" && !d.hasNew {
		// The constructor a container calls, also the type's static New.
		if !e.bare {
			w.gen("\n"+lineDirective(f.at(d.nameOff)), d.nameOff)
		}
		w.gen("\nfunc "+ctor+"("+strings.Join(d.ctorParams, ", ")+") *"+d.name+" {\n\treturn &"+d.name+
			"{"+strings.Join(d.ctorInits, ", ")+"}\n}\n", d.nameOff)
	}
	if registered {
		decl := ctor
		if ctor == "nil" {
			decl = "(*" + d.name + ")(nil)"
		}
		for _, u := range d.uses {
			if u.kind == decoRegistry {
				f.regs = append(f.regs, registration{u: u, decl: decl, uses: d.uses})
			}
		}
	}
	if len(advice) > 0 {
		// The advice: each method calls it for decorators of its own.
		w.gen("\nfunc __"+d.name+"_advice() []"+f.runtime()+".Decorator {\n\treturn ", d.nameOff)
		e.writeChain(f, advice, d.nameOff, func(u *decoUse) { e.writeDecorator(f, u, "") })
		w.gen("\n}\n", d.nameOff)
	}
	if len(inits) == 0 {
		return
	}
	if bundled {
		w.gen("\n", d.nameOff)
		e.writeBundleVars(f, d.uses, "__"+d.name)
	}
	// The description type decorators take: vuka.TypeFor, inline when one
	// takes it.
	descText := func(off int) {
		w.gen(f.runtime()+".TypeFor["+d.name+"]("+ctor, off)
		for _, a := range d.attrs {
			w.gen(", ", a.start)
			e.writeAttrValues(f, []*Attr{a})
		}
		for _, u := range d.uses {
			if u.kind == decoBundle {
				w.gen(", "+u.bvar, off)
			}
		}
		w.gen(")", off)
	}
	desc := ""
	w.gen("\nfunc init() {\n", d.nameOff)
	for _, u := range inits {
		w.gen("\t", u.a.start)
		if u.twin != "" && u.kind == decoType {
			e.writeTwin(f, u, d.name)
			w.gen("\n", u.a.end)
			continue
		}
		if u.kind != decoType && u.kind != decoBundle {
			e.writeDecorator(f, u, d.name)
			w.gen("\n", u.a.end)
			continue
		}
		if desc == "" && ndesc > 1 {
			desc = "__" + d.name + "_type"
			w.gen(desc+" := ", u.a.start)
			descText(u.a.start)
			w.gen("\n\t", u.a.start)
		}
		arg := func() {
			if desc != "" {
				w.gen(desc, u.a.end)
			} else {
				descText(u.a.end)
			}
		}
		if u.kind == decoBundle {
			w.gen(u.bvar+".Decorate(", u.a.start)
			arg()
			w.gen(")\n", u.a.start)
			continue
		}
		e.writeDecorator(f, u, "")
		if !strings.HasSuffix(strings.TrimSpace(u.a.Args), ")") && u.a.Args != "" {
			w.gen("()", u.a.end)
		}
		w.gen("(", u.a.end)
		arg()
		w.gen(")\n", u.a.end)
	}
	w.gen("}\n", d.nameOff)
}

// registration is a registry decorator's call, which the file's
// registrations init makes: what it registers (the function, method
// expression or constructor), the declaration's decorators (its options
// among them) and, for a function, its parameters' names when the
// registry's package names them with Params.
type registration struct {
	u      *decoUse
	decl   string
	uses   []*decoUse
	params []string
}

// renderRegistrations writes the file's registry decorators as calls in one
// init, in source order: web.Routes.Get("/pets/{id}", ShowPet,
// web.Params("id", "store")).
func (e *engine) renderRegistrations(f *fileState) {
	if len(f.regs) == 0 {
		return
	}
	sortItems(f.regs, func(r registration) int { return r.u.a.start })
	w := &f.deco
	w.gen("\nfunc init() {\n", f.regs[0].u.a.start)
	for _, r := range f.regs {
		u, a := r.u, r.u.a
		w.gen("\t", a.start)
		if !e.bare {
			p := a.Pos
			p.Column++
			w.gen(lineDirective(p), a.nameStart)
		}
		dot := strings.LastIndexByte(a.Name, '.') + 1
		u.at = w.len()
		w.copy(a.Name[:dot], a.nameStart)
		w.gen(u.reg.field+".", a.nameStart+dot)
		w.copy(a.Name[dot:], a.nameStart+dot)
		nameEnd := a.nameStart + len(a.Name)
		args := splitArgs(string(f.src[nameEnd:a.end]))
		w.gen("(", nameEnd)
		n := 0
		sep := func(off int) {
			if n > 0 {
				w.gen(", ", off)
			}
			n++
		}
		for i, sp := range args {
			if i == u.reg.before {
				sep(a.end)
				w.gen(r.decl, a.end)
			}
			sep(nameEnd + sp[0])
			f.copySrc(w, string(f.src[nameEnd+sp[0]:nameEnd+sp[1]]), nameEnd+sp[0])
		}
		if len(args) <= u.reg.before {
			sep(a.end)
			w.gen(r.decl, a.end)
		}
		for _, o := range r.uses {
			if o.kind == decoOption {
				sep(o.a.start)
				e.writeDecorator(f, o, "")
			}
		}
		if len(r.params) > 0 && u.reg.params {
			quoted := make([]string, len(r.params))
			for i, p := range r.params {
				quoted[i] = strconv.Quote(p)
			}
			sep(a.end)
			w.gen(a.Name[:dot]+"Params("+strings.Join(quoted, ", ")+")", a.end)
		}
		w.gen(")\n", a.end)
	}
	w.gen("}\n", f.regs[len(f.regs)-1].u.a.end)
}

// splitArgs finds the arguments of a call's parenthesized argument list,
// as spans of args trimmed of space; none for "" or "()".
func splitArgs(args string) [][2]int {
	if !strings.HasPrefix(args, "(") {
		return nil
	}
	var out [][2]int
	depth, start := 0, 1
	add := func(end int) {
		s, e := start, end
		for s < e && isSpace(args[s]) {
			s++
		}
		for e > s && isSpace(args[e-1]) {
			e--
		}
		if s < e {
			out = append(out, [2]int{s, e})
		}
	}
	for i := 0; i < len(args); i++ {
		switch c := args[i]; c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				add(i)
				return out
			}
		case ',':
			if depth == 1 {
				add(i)
				start = i + 1
			}
		case '"', '\'', '`':
			for i++; i < len(args) && args[i] != c; i++ {
				if args[i] == '\\' && c != '`' {
					i++
				}
			}
		}
	}
	return out
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

// paramNames are a function's parameters' names, receiver excluded, when
// one of them is of a type only a name tells apart (a string, a number, a
// TextUnmarshaler, …): what a registry's Params takes.
func (e *engine) paramNames(d *funcDeco) []string {
	if e.pkg == nil || len(d.pnames) == 0 {
		return nil
	}
	var fn *types.Func
	if !d.method {
		fn, _ = e.pkg.Scope().Lookup(d.name).(*types.Func)
	} else if tn, ok := e.pkg.Scope().Lookup(strings.TrimPrefix(d.rtype, "*")).(*types.TypeName); ok {
		fn, _ = pkgMethod(tn.Type(), e.pkg, d.name)
	}
	if fn == nil {
		return nil
	}
	ps := fn.Type().(*types.Signature).Params()
	for i := range ps.Len() {
		if byName(ps.At(i).Type()) {
			return d.pnames
		}
	}
	return nil
}

// byName reports whether a parameter of type t can only be bound by its
// name: a basic type, text unmarshaled, or a slice, array or pointer of one.
func byName(t types.Type) bool {
	if obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(t), true, nil, "UnmarshalText"); obj != nil {
		return true
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return true
	case *types.Slice:
		return byName(u.Elem())
	case *types.Array:
		return byName(u.Elem())
	case *types.Pointer:
		return byName(u.Elem())
	}
	return false
}

// declares reports whether the file registers declarations at init
// (Output.Declares): a declarer or a composed decorator — which may hold
// declarers — on a function, or a type decorator or a composed decorator on
// a type. Call and typed decorators only wrap calls, and typed attributes
// are metadata, so neither counts.
func (f *fileState) declares() bool {
	for _, d := range f.decos {
		switch d := d.(type) {
		case *funcDeco:
			if d.init || d.generic {
				continue // a declarer there is an error; nothing registers
			}
			for _, u := range d.uses {
				if u.decided && (u.kind == decoDecl || u.kind == decoBundle || u.kind == decoRegistry) {
					return true
				}
			}
		case *typeDeco:
			for _, u := range d.uses {
				if u.decided && (u.kind == decoType || u.kind == decoBundle || u.kind == decoRegistry) {
					return true
				}
			}
		}
	}
	return false
}

// declaresMethod marks a type whose embedders register something at init, so
// a package declaring them is auto-linked like one using a declarer:
//
//	func (Base[Self]) vukaDeclares() {}
const declaresMethod = "vukaDeclares"

// embedsDeclaring reports whether one of f's struct types embeds, by value or
// pointer, a type with the vukaDeclares marker: an ORM model embedding
// orm.Base registers itself, so its package is linked into the program.
func (e *engine) embedsDeclaring(f *fileState) bool {
	if e.pkg == nil || strings.HasSuffix(f.name, "_test.vuka") {
		return false
	}
	for _, d := range f.ast.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.TYPE {
			continue
		}
		for _, s := range g.Specs {
			tn, ok := e.pkg.Scope().Lookup(s.(*ast.TypeSpec).Name.Name).(*types.TypeName)
			if !ok {
				continue
			}
			st, ok := tn.Type().Underlying().(*types.Struct)
			if !ok {
				continue
			}
			for i := range st.NumFields() {
				if st.Field(i).Embedded() && declaring(st.Field(i).Type()) {
					return true
				}
			}
		}
	}
	return false
}

// declaring reports whether t (or what it points to) has the vukaDeclares
// marker: an unexported method without parameters or results, on t or, for
// an instance of a generic type, on its origin.
func declaring(t types.Type) bool {
	if t == nil {
		return false
	}
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return false
	}
	for _, c := range []*types.Named{n, n.Origin()} {
		obj, _, _ := types.LookupFieldOrMethod(c, true, c.Obj().Pkg(), declaresMethod)
		if fn, ok := obj.(*types.Func); ok {
			if sig := fn.Type().(*types.Signature); sig.Params().Len() == 0 && sig.Results().Len() == 0 {
				return true
			}
		}
	}
	return false
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
			f.copySrc(w, text, a.nameStart)
		} else {
			w.gen("*new(", a.start)
			w.copy(text, a.nameStart)
			w.gen(")", a.end)
		}
	}
}

// writeParamAttrs writes one parameter's attributes as a []any literal, or
// none when it has none.
func (e *engine) writeParamAttrs(f *fileState, attrs []*Attr, none string, off int) {
	w := &f.deco
	if len(attrs) == 0 {
		w.gen(none, off)
		return
	}
	w.gen("[]any{", attrs[0].start)
	for i, a := range attrs {
		if i > 0 {
			w.gen(", ", a.start)
		}
		f.writeFieldAttr(w, a)
	}
	w.gen("}", attrs[len(attrs)-1].end)
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
		f.copySrc(w, args, nameEnd)
		if u.called {
			w.gen("()", a.end)
		}
	case strings.HasPrefix(args, "["):
		w.gen("["+typeArg+", ", nameEnd)
		f.copySrc(w, args[1:], nameEnd+1)
	default:
		w.gen("["+typeArg+"]", nameEnd)
		f.copySrc(w, args, nameEnd)
	}
	if typeArg != "" && !strings.HasSuffix(strings.TrimSpace(args), ")") {
		w.gen("()", a.end)
	}
}

// writeTwin writes a type decorator as its generic twin: @ui.Island(args)
// on Board is ui.RegisterIsland[Board](args).
func (e *engine) writeTwin(f *fileState, u *decoUse, typeName string) {
	w, a := &f.deco, u.a
	if !e.bare {
		p := a.Pos
		p.Column++
		w.gen(lineDirective(p), a.nameStart)
	}
	dot := strings.LastIndexByte(a.Name, '.') + 1
	nameEnd := a.nameStart + len(a.Name)
	u.at = w.len()
	w.copy(a.Name[:dot], a.nameStart)
	w.gen(u.twin+"["+typeName+"]", a.nameStart+dot)
	args := string(f.src[nameEnd:a.end])
	if strings.HasPrefix(strings.TrimSpace(args), "(") {
		f.copySrc(w, args, nameEnd)
	} else {
		w.gen("()", a.end)
	}
}

// registerTwin is the name of the generic twin of the type decorator fn:
// Register<Name>[T any] in its package, taking what fn takes and returning
// nothing, which a person calls to do what the decorator does
// (ui.RegisterIsland[Chart]() for @ui.Island). "" when there is none.
func registerTwin(obj types.Object) string {
	fn, ok := obj.(*types.Func)
	if !ok || fn.Pkg() == nil {
		return ""
	}
	sig := fn.Type().(*types.Signature)
	twin, ok := fn.Pkg().Scope().Lookup("Register" + fn.Name()).(*types.Func)
	if !ok || !twin.Exported() {
		return ""
	}
	ts := twin.Type().(*types.Signature)
	if ts.TypeParams().Len() != 1 || ts.Results().Len() != 0 || ts.Variadic() != sig.Variadic() || ts.Params().Len() != sig.Params().Len() {
		return ""
	}
	for i := range ts.Params().Len() {
		if !types.Identical(ts.Params().At(i).Type(), sig.Params().At(i).Type()) {
			return ""
		}
	}
	return twin.Name()
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
		var fd *funcDeco
		switch d := d.(type) {
		case *funcDeco:
			us, fd = d.uses, d
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
			if r := e.registryOf(f, u); r != nil {
				switch {
				case fd != nil && fd.init:
					e.errs.add("E0213", u.a.Pos, "@%s registers the declaration it is on, which needs a function it can refer to; init can't be", u.a.Name)
				case fd != nil && fd.generic:
					e.errs.add("E0213", u.a.Pos, "@%s registers the declaration it is on, which needs a concrete function; %s is generic", u.a.Name, fd.qualName)
				}
				u.reg, u.t = r, t
				if u.kind != decoRegistry {
					u.kind, changed = decoRegistry, true
				}
				continue
			}
			if r := factoryResult(t); r != nil && u.a.Args == "" {
				// @tx for tx(opts ...Opt): called with no arguments.
				u.called, t, changed = true, r, true
			}
			u.t = t
			kind := decoTyped
			switch {
			case isRuntimeNamed(t, "Bundle"):
				kind = decoBundle
			case isRuntimeFunc(t, "Call"):
				kind = decoCall // on a type: advice for its methods
			case isType && isRuntimeFunc(t, "Type"):
				kind = decoType
				u.twin = registerTwin(e.decoratorObj(f, u))
			case isRuntimeFunc(t, "Decl"):
				kind = decoDecl
				switch {
				case isType:
					e.errs.add("E0213", u.a.Pos, "@%s is a declarer, which takes a function; a type's decorator takes a *vuka.Type", u.a.Name)
				case fd.init:
					e.errs.add("E0213", u.a.Pos, "@%s is a declarer, which needs a function it can refer to; init can't be", u.a.Name)
				case fd.generic:
					e.errs.add("E0213", u.a.Pos, "@%s is a declarer, which needs a concrete function; %s is generic", u.a.Name, fd.qualName)
				}
			}
			if kind != u.kind {
				u.kind, changed = kind, true
			}
		}
		changed = options(us) || changed
	}
	if changed {
		e.render(f)
		e.progress = true
	}
	for _, d := range f.decos {
		if td, ok := d.(*typeDeco); ok && !td.advised {
			for _, u := range td.uses {
				if u.decided && isCallLike(u.kind) && !td.advised {
					e.advise(td)
				}
			}
		}
	}
}

// options makes options of a registry decorator the other decorators on
// its declaration whose values its method takes as options, a vuka.Bundle
// excepted (it decorates the declaration itself): @web.Use(mw) beside
// @web.Get("/x") is web.Routes.Get("/x", F, web.Use(mw)). It reports
// whether a decorator's form changed.
func options(us []*decoUse) bool {
	changed := false
	for _, u := range us {
		if !u.decided || u.kind == decoRegistry || u.kind == decoBundle || u.t == nil {
			continue
		}
		opt := false
		for _, r := range us {
			if r.kind == decoRegistry && r.reg.elem != nil && types.AssignableTo(u.t, r.reg.elem) {
				opt = true
			}
		}
		switch {
		case opt && u.kind != decoOption:
			u.kind, changed = decoOption, true
		case !opt && u.kind == decoOption:
			u.kind, u.decided, changed = decoTyped, false, true
		}
	}
	return changed
}

// advise gives a type's call decorators — its advice — to each exported
// method the package's Vuka files declare for it, outside the method's own
// decorators. A method marked @vuka.NoAdvice is left out.
func (e *engine) advise(td *typeDeco) {
	td.advised = true
	fn := "__" + td.name + "_advice"
	for _, g := range e.vuka {
		touched := false
		for _, decl := range g.ast.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || g.off(fd.Pos()) >= g.body || recvBase(fd) != td.name {
				continue
			}
			nameOff := g.orig(fd.Name.Pos())
			if !token.IsExported(srcIdent(g.src, nameOff)) || e.noAdvice(g, g.orig(fd.Type.Func)) {
				continue
			}
			var d *funcDeco
			for _, x := range g.decos {
				if x, ok := x.(*funcDeco); ok && x.nameOff == nameOff {
					d = x
				}
			}
			if d == nil {
				d = e.decorateFunc(g, fd, nil)
			}
			d.advice = append(d.advice, fn)
			touched = true
		}
		if touched {
			e.render(g)
		}
	}
	e.progress = true
}

// noAdvice reports whether the declaration whose keyword is at off carries
// @vuka.NoAdvice.
func (e *engine) noAdvice(f *fileState, off int) bool {
	for _, a := range f.attrs {
		if a.declOff == off && a.kind == attrTyped && e.isRuntimeName(f, a.Name, "NoAdvice") {
			return true
		}
	}
	return false
}

// isRuntimeName reports whether name, as f writes it, is the runtime's sel.
func (e *engine) isRuntimeName(f *fileState, name, sel string) bool {
	pkg, s, ok := strings.Cut(name, ".")
	if !ok || s != sel {
		return false
	}
	p := e.importedAs(f, pkg)
	return p != nil && p.Path() == RuntimePath
}

// decoratorObj is what a decorator's name denotes, once type-checked.
func (e *engine) decoratorObj(f *fileState, u *decoUse) types.Object {
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
	return obj
}

// decoratorType is the type of the value a decorator denotes: its name's type,
// or for @name(args) what calling it returns.
func (e *engine) decoratorType(f *fileState, u *decoUse) types.Type {
	obj := e.decoratorObj(f, u)
	if obj == nil {
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

// registryOf is how a decorator is called when it names a method of its
// package's registry: an exported package-level variable whose type has
// the marker method vukaRegistry() (declared in that package). The method's
// last parameter before its options takes the declaration.
func (e *engine) registryOf(f *fileState, u *decoUse) *registryUse {
	obj, ok := e.decoratorObj(f, u).(*types.Func)
	if !ok || obj.Pkg() == nil || obj.Type().(*types.Signature).Recv() != nil {
		return nil
	}
	pkg := obj.Pkg()
	reg, ok := e.registries[pkg]
	if !ok {
		reg = registryVar(pkg)
		if e.registries == nil {
			e.registries = map[*types.Package]*types.Var{}
		}
		e.registries[pkg] = reg
	}
	if reg == nil {
		return nil
	}
	m, ok := pkgMethod(reg.Type(), pkg, obj.Name())
	if !ok || !m.Exported() {
		return nil
	}
	sig := m.Type().(*types.Signature)
	n := sig.Params().Len()
	r := &registryUse{field: reg.Name(), before: n - 1}
	if sig.Variadic() {
		r.before--
		r.elem = sig.Params().At(n - 1).Type().(*types.Slice).Elem()
	}
	if r.before < 0 {
		return nil
	}
	if r.elem != nil {
		if pf, ok := pkg.Scope().Lookup("Params").(*types.Func); ok {
			ps := pf.Type().(*types.Signature)
			if ps.Variadic() && ps.Params().Len() == 1 && ps.Results().Len() == 1 &&
				types.Identical(ps.Params().At(0).Type(), types.NewSlice(types.Typ[types.String])) &&
				types.AssignableTo(ps.Results().At(0).Type(), r.elem) {
				r.params = true
			}
		}
	}
	return r
}

// registryVar is pkg's registry, or nil.
func registryVar(pkg *types.Package) *types.Var {
	for _, name := range pkg.Scope().Names() {
		v, ok := pkg.Scope().Lookup(name).(*types.Var)
		if !ok || !v.Exported() {
			continue
		}
		if m, ok := pkgMethod(v.Type(), pkg, "vukaRegistry"); ok {
			if sig := m.Type().(*types.Signature); sig.Params().Len() == 0 && sig.Results().Len() == 0 {
				return v
			}
		}
	}
	return nil
}

func pkgMethod(t types.Type, pkg *types.Package, name string) (*types.Func, bool) {
	obj, _, _ := types.LookupFieldOrMethod(t, true, pkg, name)
	m, ok := obj.(*types.Func)
	return m, ok
}

// factoryResult is what calling t with no arguments returns, when t is a
// function whose parameters are all optional (none, or one ...T) and which
// returns one value: a decorator written bare, @tx, is then tx().
func factoryResult(t types.Type) types.Type {
	sig, ok := t.Underlying().(*types.Signature)
	if !ok || sig.TypeParams().Len() > 0 || sig.Results().Len() != 1 {
		return nil
	}
	if n := sig.Params().Len(); n > 1 || n == 1 && !sig.Variadic() {
		return nil
	}
	return sig.Results().At(0).Type()
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

// embeddedName is an embedded field's name: its type's name.
func embeddedName(t ast.Expr) string {
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.SelectorExpr:
			return x.Sel.Name
		case *ast.IndexExpr:
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		case *ast.Ident:
			return x.Name
		default:
			return "_"
		}
	}
}

func genericRecv(fd *ast.FuncDecl) bool {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return false
	}
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
