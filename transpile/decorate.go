package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// decorate lowers every decorator, in the first round, and reports whether there
// were any. A decorator is a Go expression yielding a function that wraps the
// declaration:
//
//	@trace.Log("orders")
//	func charge(ctx context.Context, id int) (Receipt, error) { … }
//
// keeps the name charge for a wrapper that builds trace.Log("orders")(__charge)
// once, on first call, and calls it; the body moves to __charge. A method is
// decorated as its method expression, receiver first. A type's decorators are
// generic functions called at init with the type as their first type argument:
// @orm.Table("users") on type User runs orm.Table[User]("users").
func (e *engine) decorate() bool {
	any := false
	for _, f := range e.vuka {
		groups := map[ast.Decl][]*Attr{}
		var order []ast.Decl
		for _, a := range f.attrs {
			if a.bare && !e.namesType(f, a.Name) {
				a.kind, a.Decorator, a.bare = attrDecorator, true, false
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
		f.makeTrailer(e.bare)
	}
	return any
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
	name := fd.Name.Name
	nameOff := f.orig(fd.Name.Pos())
	if f.decorated == nil {
		f.decorated = map[int]string{}
	}

	// An overload keeps its mangled name for the wrapper; its body moves on.
	if set := e.sets[declKey(fd)]; set != nil {
		for _, o := range set.list {
			if o.nameOff == nameOff {
				name = o.mangled
			}
		}
		for i, ed := range f.fixed {
			if ed.start == nameOff {
				f.fixed = append(f.fixed[:i], f.fixed[i+1:]...)
				break
			}
		}
	}
	impl := "__" + name
	f.add(nameOff, nameOff+len(fd.Name.Name), impl)
	f.decorated[attrs[0].declOff] = name
	w := &f.deco

	// init runs once, so it needs no cache: init() calls the decorated body.
	if fd.Recv == nil && name == "init" {
		w.gen("\n", nameOff)
		if !e.bare {
			w.gen(lineDirective(f.at(nameOff)), nameOff)
		}
		w.gen("func init() {\n\t", nameOff)
		for _, a := range attrs {
			e.writeDecorator(f, a, "")
			w.gen("(", a.end)
		}
		w.gen(impl+strings.Repeat(")", len(attrs))+"()\n}\n", nameOff)
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
	var types_, params, args []string
	for _, field := range fd.Type.Params.List {
		t := e.qualText(f, field.Type)
		_, variadic := field.Type.(*ast.Ellipsis)
		for k := range max(1, len(field.Names)) {
			p := "a" + itoa(len(params))
			if named {
				p = field.Names[k].Name
			}
			types_ = append(types_, t)
			params = append(params, p+" "+t)
			if variadic {
				p += "..."
			}
			args = append(args, p)
		}
	}
	results := ""
	if fd.Type.Results != nil {
		results = " " + e.qualText(f, fd.Type.Results)
	}

	// A generic function's wrapper instantiates the body with its own type
	// parameters, and caches one decorated function per instantiation.
	tparams, targs := "", ""
	if tp := fd.Type.TypeParams; tp != nil {
		tparams = e.qualText(f, tp)
		var names []string
		for _, field := range tp.List {
			for _, n := range field.Names {
				names = append(names, n.Name)
			}
		}
		targs = "[" + strings.Join(names, ", ") + "]"
	}
	generic := tparams != "" || fd.Recv != nil && genericRecv(fd)

	recv, ref, state := "", impl+targs, "__"+name+"_dec"
	if fd.Recv != nil {
		field := fd.Recv.List[0]
		rt := e.qualText(f, field.Type)
		rn := "r"
		if len(field.Names) > 0 && field.Names[0].Name != "_" {
			rn = field.Names[0].Name
		}
		recv = "(" + rn + " " + rt + ") "
		types_ = append([]string{rt}, types_...)
		args = append([]string{rn}, args...)
		ref = "(" + rt + ")." + impl
		state = "__" + recvBase(fd) + "_" + name + "_dec"
	}
	fn := "func(" + strings.Join(types_, ", ") + ")" + results

	w.gen("\n", nameOff)
	if fd.Doc != nil {
		if doc := trimDoc(f.nodeText(fd.Doc)); doc != "" {
			w.gen(doc+"\n", nameOff)
		}
	}
	rt := f.runtime()
	if generic {
		w.gen("var "+state+" "+rt+".Instances\n\n", nameOff)
	} else {
		w.gen("var "+state+" "+rt+".Decorated["+fn+"]\n\n", nameOff)
	}
	if !e.bare {
		w.gen(lineDirective(f.at(nameOff)), nameOff)
	}
	w.gen("func "+recv+name+tparams+"("+strings.Join(params, ", ")+")"+results+" {\n\t", nameOff)
	if results != "" {
		w.gen("return ", nameOff)
	}
	if generic {
		w.gen(rt+".Instance(&"+state+", func() "+fn+" {\n\t\treturn ", nameOff)
	} else {
		w.gen(state+".Get(func() "+fn+" {\n\t\treturn ", nameOff)
	}
	for _, a := range attrs {
		e.writeDecorator(f, a, "")
		w.gen("(", a.end)
	}
	w.gen(ref+strings.Repeat(")", len(attrs))+"\n\t})("+strings.Join(args, ", ")+")\n}\n", nameOff)
}

// decorateType runs a type's decorators at init. On a group, they apply to
// every type in it.
func (e *engine) decorateType(f *fileState, gd *ast.GenDecl, attrs []*Attr) {
	w := &f.deco
	for _, spec := range gd.Specs {
		ts := spec.(*ast.TypeSpec)
		if ts.TypeParams != nil {
			e.errs.add(attrs[0].Pos, "%s is generic; a type decorator needs a type it can name, so instantiate it in a decorated alias: type IntBox = Box[int]", ts.Name.Name)
			continue
		}
		nameOff := f.orig(ts.Name.Pos())
		w.gen("\nfunc init() {\n", nameOff)
		for _, a := range attrs {
			w.gen("\t", a.start)
			e.writeDecorator(f, a, ts.Name.Name)
			w.gen("\n", a.end)
		}
		w.gen("}\n", nameOff)
	}
}

// writeDecorator copies a decorator's text into f's wrappers. With typeArg set
// (a type decorator) the type goes in as the first type argument, and a bare
// decorator is called with no arguments.
func (e *engine) writeDecorator(f *fileState, a *Attr, typeArg string) {
	w := &f.deco
	if !e.bare {
		p := a.Pos
		p.Column++
		w.gen(lineDirective(p), a.nameStart)
	}
	nameEnd := a.nameStart + len(a.Name)
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
