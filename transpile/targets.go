package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// The places an attribute can be written, as the runtime's Targets bits.
const (
	onFunc = 1 << iota
	onMethod
	onType
	onField
	onParam
	onVar
)

var (
	targetNames = []string{"functions", "methods", "types", "fields", "parameters", "vars and consts"}
	targetOne   = []string{"a function", "a method", "a type", "a field", "a parameter", "a var or const"}
)

// targetsMethod is the method recording an attribute type's targets: its
// result is an array whose length is them, so other packages read them from
// the type alone.
const targetsMethod = "vukaTargets"

// renderTargets gives each type written with @vuka.Targets(…) its targets
// method:
//
//	func (Char) vukaTargets() [vuka.OnField]struct{} { return [vuka.OnField]struct{}{} }
func (e *engine) renderTargets(f *fileState) {
	w := &f.deco
	for _, a := range f.attrs {
		if a.kind != attrTyped || a.Field || a.Param || !e.isRuntimeName(f, a.Name, "Targets") {
			continue
		}
		gd, ok := a.decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE || !strings.HasPrefix(a.Args, "(") {
			continue // reported by checkTargets
		}
		for _, s := range gd.Specs {
			ts := s.(*ast.TypeSpec)
			recv := ts.Name.Name
			if tp := ts.TypeParams; tp != nil {
				n := 0
				for _, field := range tp.List {
					n += len(field.Names)
				}
				recv += "[" + strings.TrimSuffix(strings.Repeat("_, ", n), ", ") + "]"
			}
			arr := func() {
				w.gen("[", a.start)
				f.copySrc(w, a.Args[1:len(a.Args)-1], a.nameStart+len(a.Name)+1)
				w.gen("]struct{}", a.end)
			}
			w.gen("\nfunc ("+recv+") "+targetsMethod+"() ", a.start)
			arr()
			w.gen(" { return ", a.start)
			arr()
			w.gen("{} }\n", a.end)
		}
	}
}

// checkTargets reports each attribute written where its type's targets
// don't allow it.
func (e *engine) checkTargets() {
	for _, f := range e.vuka {
		for _, a := range f.attrs {
			var where int
			switch {
			case a.Field:
				where = onField
			case a.Param:
				where = onParam
			case a.kind != attrTyped:
				continue
			default:
				switch d := a.decl.(type) {
				case *ast.FuncDecl:
					where = onFunc
					if d.Recv != nil {
						where = onMethod
					}
				case *ast.GenDecl:
					where = onVar
					if d.Tok == token.TYPE {
						where = onType
					}
				default:
					continue
				}
			}
			if where != onType && a.kind == attrTyped && e.isRuntimeName(f, a.Name, "Targets") || where == onType && e.isRuntimeName(f, a.Name, "Targets") && !strings.HasPrefix(a.Args, "(") {
				e.errs.add(a.Pos, "@%s goes on an attribute type: @%s(vuka.OnField) type Char struct{ … }", a.Name, a.Name)
				continue
			}
			t, name := e.attrType(f, a)
			if t == nil {
				continue
			}
			allowed, ok := targetsOf(t)
			if !ok || allowed&where != 0 {
				continue
			}
			var can []string
			for i, n := range targetNames {
				if allowed&(1<<i) != 0 {
					can = append(can, n)
				}
			}
			e.errs.add(a.Pos, "@%s can't go on %s; %s is for %s", a.Name, targetOne[indexBit(where)], name, strings.Join(can, " and "))
		}
	}
}

func indexBit(b int) int {
	i := 0
	for b > 1 {
		b >>= 1
		i++
	}
	return i
}

// targetsOf reads the targets an attribute type declares.
func targetsOf(t types.Type) (int, bool) {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return 0, false
	}
	obj, _, _ := types.LookupFieldOrMethod(n, false, n.Obj().Pkg(), targetsMethod)
	fn, ok := obj.(*types.Func)
	if !ok {
		return 0, false
	}
	sig := fn.Type().(*types.Signature)
	if sig.Results().Len() != 1 {
		return 0, false
	}
	arr, ok := sig.Results().At(0).Type().(*types.Array)
	if !ok {
		return 0, false
	}
	return int(arr.Len()), true
}

// attrType is the type of an attribute's value, and the type's name as
// messages give it: the type an attribute names, or what the function it
// calls returns.
func (e *engine) attrType(f *fileState, a *Attr) (types.Type, string) {
	pkgName, sel, qualified := strings.Cut(a.Name, ".")
	var obj types.Object
	if !qualified {
		obj = e.pkg.Scope().Lookup(a.Name)
	} else if p := e.importedAs(f, pkgName); p != nil {
		obj = p.Scope().Lookup(sel)
	}
	switch o := obj.(type) {
	case *types.TypeName:
		return o.Type(), a.Name
	case *types.Func:
		if sig := o.Type().(*types.Signature); sig.Results().Len() == 1 && strings.HasPrefix(a.Args, "(") {
			r := sig.Results().At(0).Type()
			return r, e.display(r)
		}
	}
	return nil, ""
}
