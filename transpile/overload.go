package transpile

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"strings"
)

// An overload is one declaration of an overloaded function or method. It is
// renamed to a mangled name, and every call is pointed at the overload whose
// parameters fit the arguments' static types best.
type overload struct {
	file    *fileState
	decl    *ast.FuncDecl // from the first round
	nameOff int           // src offset of the declared name
	mangled string
	sig     *types.Signature // from the latest round
}

type overloadSet struct {
	name string // function or method name
	recv string // receiver's base type name; "" for a function
	list []*overload
}

func (s *overloadSet) display() string {
	if s.recv != "" {
		return s.recv + "." + s.name
	}
	return s.name
}

func recvBase(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) == 0 {
		return ""
	}
	t := fd.Recv.List[0].Type
	for {
		switch x := t.(type) {
		case *ast.StarExpr:
			t = x.X
		case *ast.ParenExpr:
			t = x.X
		case *ast.IndexExpr:
			t = x.X
		case *ast.IndexListExpr:
			t = x.X
		case *ast.Ident:
			return x.Name
		default:
			return ""
		}
	}
}

func declKey(fd *ast.FuncDecl) string {
	if r := recvBase(fd); r != "" {
		return r + "." + fd.Name.Name
	}
	return fd.Name.Name
}

// findOverloads collects the functions and methods declared more than once, and
// renames each declaration to its mangled name.
func findOverloads(files []*fileState, errs *ErrorList) map[string]*overloadSet {
	sets := map[string]*overloadSet{}
	var order []string
	for _, f := range files {
		for _, d := range f.ast.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Name.Name == "_" || fd.Recv == nil && fd.Name.Name == "init" {
				continue
			}
			key := declKey(fd)
			s := sets[key]
			if s == nil {
				s = &overloadSet{name: fd.Name.Name, recv: recvBase(fd)}
				sets[key] = s
				order = append(order, key)
			}
			s.list = append(s.list, &overload{file: f, decl: fd, nameOff: f.orig(fd.Name.Pos())})
		}
	}
	for _, key := range order {
		s := sets[key]
		if len(s.list) < 2 {
			delete(sets, key)
			continue
		}
		seen := map[string]*overload{}
		for _, o := range s.list {
			if o.decl.Type.TypeParams != nil {
				errs.add(o.file.nodePos(o.decl.Name.Pos()), "generic functions can't be overloaded yet")
			}
			o.mangled = mangle(o.decl)
			if prev := seen[o.mangled]; prev != nil {
				if paramTypes(prev) == paramTypes(o) {
					errs.add(o.file.nodePos(o.decl.Name.Pos()), "%s is already declared with parameters (%s) at %s",
						s.display(), paramTypes(o), prev.file.nodePos(prev.decl.Name.Pos()))
					continue
				}
				for n := 2; seen[o.mangled] != nil; n++ {
					o.mangled = mangle(o.decl) + "_" + itoa(n)
				}
			}
			seen[o.mangled] = o
		}
	}
	if len(*errs) == 0 {
		for _, s := range sets {
			for _, o := range s.list {
				o.file.add(o.nameOff, o.nameOff+len(s.name), o.mangled)
			}
		}
	}
	return sets
}

func paramTypes(o *overload) string {
	var ts []string
	for _, field := range o.decl.Type.Params.List {
		for range max(1, len(field.Names)) {
			ts = append(ts, types.ExprString(field.Type))
		}
	}
	return strings.Join(ts, ", ")
}

// mangle names an overload after its parameter types: area(c Circle) is
// area__Circle, scale(s *Shape, by float64) is scale__pShape_float64.
func mangle(fd *ast.FuncDecl) string {
	var keys []string
	for _, field := range fd.Type.Params.List {
		k := typeKey(field.Type)
		for range max(1, len(field.Names)) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return fd.Name.Name + "__0"
	}
	return fd.Name.Name + "__" + strings.Join(keys, "_")
}

func typeKey(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.ParenExpr:
		return typeKey(t.X)
	case *ast.StarExpr:
		return "p" + typeKey(t.X)
	case *ast.SelectorExpr:
		return typeKey(t.X) + "_" + t.Sel.Name
	case *ast.Ellipsis:
		return "v" + typeKey(t.Elt)
	case *ast.ArrayType:
		if t.Len == nil {
			return "s" + typeKey(t.Elt)
		}
		return "a" + sanitize(types.ExprString(t.Len)) + typeKey(t.Elt)
	case *ast.MapType:
		return "m" + typeKey(t.Key) + "_" + typeKey(t.Value)
	case *ast.ChanType:
		return "c" + typeKey(t.Value)
	case *ast.FuncType:
		return "fn"
	case *ast.InterfaceType:
		if t.Methods == nil || len(t.Methods.List) == 0 {
			return "any"
		}
		return "iface"
	case *ast.StructType:
		return "struct"
	case *ast.IndexExpr:
		return typeKey(t.X) + "_" + typeKey(t.Index)
	case *ast.IndexListExpr:
		k := typeKey(t.X)
		for _, x := range t.Indices {
			k += "_" + typeKey(x)
		}
		return k
	}
	return sanitize(types.ExprString(e))
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '_' || 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// resolveCalls points each call of an overloaded name whose argument types are
// now known at the overload that fits them best.
func (e *engine) resolveCalls(f *fileState) {
	if len(e.sets) == 0 {
		return
	}
	ast.Inspect(f.ast, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		set, id := e.target(call.Fun)
		if set == nil {
			return true
		}
		off := f.orig(id.Pos())
		if f.done[off] || f.off(id.Pos()) >= f.body {
			return true
		}
		args, ok := e.args(call)
		if !ok {
			return true
		}
		f.done[off] = true
		o, msg := e.pick(set, args, call.Ellipsis.IsValid())
		if msg != "" {
			e.errs.add(f.nodePos(call.Pos()), "%s", msg)
			return true
		}
		f.add(off, off+len(id.Name), o.mangled)
		e.progress = true
		return true
	})
}

// target finds the overload set a call's function expression names, if it is an
// overloaded name not resolved yet.
func (e *engine) target(fun ast.Expr) (*overloadSet, *ast.Ident) {
	switch fun := ast.Unparen(fun).(type) {
	case *ast.Ident:
		if s := e.funcs[fun.Name]; s != nil && e.info.Uses[fun] == nil && e.info.Defs[fun] == nil {
			return s, fun
		}
	case *ast.SelectorExpr:
		if !e.mnames[fun.Sel.Name] || e.info.Selections[fun] != nil {
			return nil, nil
		}
		tv, ok := e.info.Types[fun.X]
		if !ok || tv.Type == nil {
			return nil, nil
		}
		if named := namedOf(tv.Type); named != nil && named.Obj().Pkg() == e.pkg {
			if s := e.methods[named.Obj().Name()+"."+fun.Sel.Name]; s != nil {
				return s, fun.Sel
			}
		}
	}
	return nil, nil
}

func namedOf(t types.Type) *types.Named {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	n, _ := t.(*types.Named)
	return n
}

type arg struct {
	t types.Type
	v constant.Value
}

func (e *engine) args(call *ast.CallExpr) ([]arg, bool) {
	var out []arg
	for _, a := range call.Args {
		tv, ok := e.info.Types[a]
		if !ok || isInvalid(tv.Type) {
			return nil, false
		}
		if tup, ok := tv.Type.(*types.Tuple); ok {
			if len(call.Args) != 1 {
				return nil, false
			}
			for i := 0; i < tup.Len(); i++ {
				out = append(out, arg{t: tup.At(i).Type()})
			}
			continue
		}
		out = append(out, arg{tv.Type, tv.Value})
	}
	return out, true
}

// pick chooses the overload that fits best: each argument scores 0 for an
// identical type, 1 for an untyped constant whose default type is the
// parameter's, 2 for any other assignable value. Lowest total wins; a tie is
// ambiguous.
func (e *engine) pick(set *overloadSet, args []arg, spread bool) (*overload, string) {
	best := -1
	var winners []*overload
	for _, o := range set.list {
		if o.sig == nil {
			continue
		}
		score, ok := matchSig(o.sig, args, spread)
		switch {
		case !ok:
		case best < 0 || score < best:
			best, winners = score, []*overload{o}
		case score == best:
			winners = append(winners, o)
		}
	}
	switch len(winners) {
	case 1:
		return winners[0], ""
	case 0:
		return nil, fmt.Sprintf("no overload of %s accepts (%s)%s", set.display(), e.argList(args), e.candidates(set.list))
	}
	return nil, fmt.Sprintf("call of %s with (%s) is ambiguous%s", set.display(), e.argList(args), e.candidates(winners))
}

func matchSig(sig *types.Signature, args []arg, spread bool) (int, bool) {
	params := sig.Params()
	n := params.Len()
	switch {
	case spread:
		if !sig.Variadic() || len(args) != n {
			return 0, false
		}
	case sig.Variadic():
		if len(args) < n-1 {
			return 0, false
		}
	case len(args) != n:
		return 0, false
	}
	score := 0
	for i, a := range args {
		p := params.At(min(i, n-1)).Type()
		if sig.Variadic() && !spread && i >= n-1 {
			p = p.(*types.Slice).Elem()
		}
		s, ok := argScore(a, p)
		if !ok {
			return 0, false
		}
		score += s
	}
	return score, true
}

func argScore(a arg, p types.Type) (int, bool) {
	if types.Identical(a.t, p) {
		return 0, true
	}
	if b, ok := a.t.(*types.Basic); ok && b.Info()&types.IsUntyped != 0 {
		if a.v != nil && !representable(a.v, p) {
			return 0, false
		}
		if types.Identical(types.Default(a.t), p) {
			return 1, true
		}
	}
	if types.AssignableTo(a.t, p) {
		return 2, true
	}
	return 0, false
}

func representable(v constant.Value, t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	if !ok {
		return true
	}
	info := b.Info()
	switch {
	case info&types.IsInteger != 0:
		return constant.ToInt(v).Kind() == constant.Int
	case info&types.IsFloat != 0:
		k := constant.ToFloat(v).Kind()
		return k == constant.Float || k == constant.Int
	case info&types.IsComplex != 0:
		return constant.ToComplex(v).Kind() != constant.Unknown
	case info&types.IsString != 0:
		return v.Kind() == constant.String
	case info&types.IsBoolean != 0:
		return v.Kind() == constant.Bool
	}
	return true
}

func (e *engine) argList(args []arg) string {
	q := types.RelativeTo(e.pkg)
	s := make([]string, len(args))
	for i, a := range args {
		s[i] = types.TypeString(a.t, q)
	}
	return strings.Join(s, ", ")
}

func (e *engine) candidates(list []*overload) string {
	q := func(p *types.Package) string {
		if p == e.pkg {
			return ""
		}
		return p.Name()
	}
	var b strings.Builder
	for _, o := range list {
		if o.sig != nil {
			fmt.Fprintf(&b, "\n\t%s at %s", strings.TrimPrefix(types.TypeString(o.sig, q), "func"), o.file.at(o.nameOff))
		}
	}
	return b.String()
}

// unresolvedOverloads reports every overloaded name still not pointing at an
// overload, and whether any was a call stuck on unknown argument types.
func (e *engine) unresolvedOverloads() bool {
	if len(e.sets) == 0 {
		return false
	}
	stuck := false
	for _, f := range e.vuka {
		calls := map[*ast.Ident]bool{}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if _, id := e.target(call.Fun); id != nil {
					calls[id] = true
				}
			}
			return true
		})
		report := func(set *overloadSet, id *ast.Ident) {
			if f.done[f.orig(id.Pos())] {
				return
			}
			if calls[id] {
				stuck = true
				e.errs.add(f.nodePos(id.Pos()), "can't choose an overload of %s: an argument's type is unknown", set.display())
			} else {
				e.errs.add(f.nodePos(id.Pos()), "%s is overloaded and can't be used as a value; call it, or name one overload with @export", set.display())
			}
		}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.SelectorExpr:
				if set, id := e.target(n); set != nil {
					report(set, id)
				}
				ast.Inspect(n.X, func(m ast.Node) bool {
					if id, ok := m.(*ast.Ident); ok {
						if set, id := e.target(id); set != nil {
							report(set, id)
						}
					}
					return true
				})
				return false
			case *ast.Ident:
				if set, id := e.target(n); set != nil {
					report(set, id)
				}
			}
			return true
		})
	}
	return stuck
}

func (e *engine) mentionsOverload(msg string) bool {
	for name := range e.funcs {
		if strings.Contains(msg, "undefined: "+name) {
			return true
		}
	}
	for name := range e.mnames {
		if strings.Contains(msg, "no field or method "+name) {
			return true
		}
	}
	return false
}
