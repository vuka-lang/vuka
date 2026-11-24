package main

import (
	"go/token"
	"go/types"
	"strings"
)

// assignElem is T of a ui.Assign[T], the type a vuka file's unexported field
// is held in; any other type is itself.
func assignElem(t types.Type) types.Type {
	if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Name() == "Assign" && n.TypeArgs().Len() == 1 {
		return n.TypeArgs().At(0)
	}
	return t
}

// fieldFold is t's exported field named name, case aside, promoted ones
// included: the field a path parameter binds.
func fieldFold(t types.Type, name string) *types.Var {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	for i := 0; i < st.NumFields(); i++ {
		if f := st.Field(i); !f.Embedded() && f.Exported() && strings.EqualFold(f.Name(), name) {
			return f
		}
	}
	for i := 0; i < st.NumFields(); i++ {
		if f := st.Field(i); f.Embedded() {
			if g := fieldFold(f.Type(), name); g != nil {
				return g
			}
		}
	}
	return nil
}

// paramKindOf says how a path parameter of type t is parsed and written.
func paramKindOf(t types.Type) (paramKind, bool) {
	if n, ok := t.(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == webPath && n.Obj().Name() == "Rest" {
		return paramRest, true
	}
	if hasMethod(t, "MarshalText") && hasMethod(types.NewPointer(t), "UnmarshalText") && !isOptionType(t) {
		return paramText, true
	}
	b, ok := t.Underlying().(*types.Basic)
	switch {
	case !ok:
	case b.Info()&types.IsUnsigned != 0:
		return paramUint, true
	case b.Info()&types.IsInteger != 0:
		return paramInt, true
	case b.Kind() == types.String:
		return paramString, true
	}
	return 0, false
}

func hasMethod(t types.Type, name string) bool {
	ms := types.NewMethodSet(t)
	sel := ms.Lookup(nil, name)
	return sel != nil
}

func isHTTPHandler(t types.Type) bool { return isNamed(t, "net/http", "Handler", false) }

// isMiddlewareType reports whether t is web.Middleware, an alias of
// func(http.Handler) http.Handler.
func isMiddlewareType(t types.Type) bool {
	sig, ok := t.Underlying().(*types.Signature)
	return ok && !sig.Variadic() && sig.Params().Len() == 1 && isHTTPHandler(sig.Params().At(0).Type()) &&
		sig.Results().Len() == 1 && isHTTPHandler(sig.Results().At(0).Type())
}

// buildsPtr reports whether ctor returns *t or (*t, error).
func buildsPtr(ctor *types.Func, t *types.Named) bool {
	res := ctor.Type().(*types.Signature).Results()
	if res.Len() == 0 || res.Len() > 2 || res.Len() == 2 && !isError(res.At(1).Type()) {
		return false
	}
	p, ok := res.At(0).Type().(*types.Pointer)
	return ok && types.Identical(p.Elem(), t)
}

func isContext(t types.Type) bool { return isNamed(t, "context", "Context", false) }

func isError(t types.Type) bool { return types.Identical(t, types.Universe.Lookup("error").Type()) }

// isNamed reports whether t is the named type path.name, or a pointer to it.
func isNamed(t types.Type, path, name string, ptr bool) bool {
	if p, ok := t.(*types.Pointer); ok && ptr {
		t = p.Elem()
	} else if ptr {
		return false
	}
	n, ok := t.(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == path && n.Obj().Name() == name
}

func scalarParam(t types.Type) bool {
	if hasMethod(types.NewPointer(t), "UnmarshalText") {
		return true
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&(types.IsInteger|types.IsFloat|types.IsString|types.IsBoolean) != 0
	case *types.Pointer:
		return scalarParam(u.Elem())
	case *types.Slice:
		return scalarParam(u.Elem())
	}
	return false
}

func structParam(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := 0; i < st.NumFields(); i++ {
		if st.Field(i).Exported() {
			return true
		}
	}
	return false
}

// isPointer reports whether t is a pointer: a pointer to a struct is a
// provided dependency (a service) or, when nothing provides it, the body;
// only the container knows which, so it is never a second body here.
func isPointer(t types.Type) bool {
	_, ok := t.(*types.Pointer)
	return ok
}

func isPtrOrIface(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Interface:
		return true
	}
	return false
}

func lookupType(pkg *types.Package, name string) types.Type {
	if obj, ok := pkg.Scope().Lookup(name).(*types.TypeName); ok {
		return obj.Type()
	}
	return nil
}

// firstPos is where a package's first declaration is, for a diagnostic
// about the package.
func firstPos(pkg *types.Package) token.Pos {
	best := token.NoPos
	for _, name := range pkg.Scope().Names() {
		if p := pkg.Scope().Lookup(name).Pos(); p.IsValid() && (!best.IsValid() || p < best) {
			best = p
		}
	}
	return best
}

func namedObj(t types.Type) *types.TypeName {
	switch t := types.Unalias(t).(type) {
	case *types.Named:
		return t.Obj()
	}
	return nil
}
