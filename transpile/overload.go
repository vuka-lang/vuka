package transpile

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"
)

// An overload is one declaration of an overloaded function or method. It is
// renamed to a mangled name, and every call is pointed at the overload whose
// parameters fit the arguments' static types best.
type overload struct {
	file    *fileState
	decl    *ast.FuncDecl
	mangled string
	sig     *types.Signature
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
			s.list = append(s.list, &overload{file: f, decl: fd})
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
				o.file.rename(o.decl.Name, o.decl.Name.Name, o.mangled, errs)
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

type resolver struct {
	files       []*fileState
	byFile      map[*ast.File]*fileState
	funcs       map[string]*overloadSet
	methods     map[string]*overloadSet
	methodNames map[string]bool
	pkg         *types.Package
	info        *types.Info
	failed      map[*ast.Ident]bool
	errs        *ErrorList
}

// resolve type-checks the package and points every call of an overloaded name at
// one overload. It repeats until no call is left whose argument types became
// known in the last round: x := area(c); scale(x) resolves in two rounds.
func resolve(fset *token.FileSet, files []*fileState, sets map[string]*overloadSet, imp types.Importer, errs *ErrorList) {
	r := &resolver{
		files:       files,
		byFile:      map[*ast.File]*fileState{},
		funcs:       map[string]*overloadSet{},
		methods:     map[string]*overloadSet{},
		methodNames: map[string]bool{},
		failed:      map[*ast.Ident]bool{},
		errs:        errs,
	}
	var astFiles []*ast.File
	for _, f := range files {
		r.byFile[f.ast] = f
		astFiles = append(astFiles, f.ast)
	}
	for key, s := range sets {
		if s.recv == "" {
			r.funcs[s.name] = s
		} else {
			r.methods[key] = s
			r.methodNames[s.name] = true
		}
	}
	for _, f := range files {
		if f.vuka {
			continue
		}
		for _, d := range f.ast.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				if s := sets[declKey(fd)]; s != nil {
					errs.add(fset.Position(fd.Name.Pos()), "%s is overloaded in Vuka source; a .go file can't declare it too", s.display())
				}
			}
		}
	}
	if len(*errs) > 0 {
		return
	}

	var typeErrs []types.Error
	conf := types.Config{
		Importer:    imp,
		FakeImportC: true,
		Error:       func(err error) { typeErrs = append(typeErrs, err.(types.Error)) },
	}
	for {
		typeErrs = nil
		r.info = &types.Info{
			Types:      map[ast.Expr]types.TypeAndValue{},
			Defs:       map[*ast.Ident]types.Object{},
			Uses:       map[*ast.Ident]types.Object{},
			Selections: map[*ast.SelectorExpr]*types.Selection{},
		}
		r.pkg, _ = conf.Check(astFiles[0].Name.Name, fset, astFiles, r.info)
		for _, s := range sets {
			for _, o := range s.list {
				if fn, ok := r.info.Defs[o.decl.Name].(*types.Func); ok {
					o.sig = fn.Type().(*types.Signature)
				}
			}
		}
		progress := false
		for _, f := range files {
			if !f.vuka {
				continue
			}
			ast.Inspect(f.ast, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok && r.call(f, call) {
					progress = true
				}
				return true
			})
		}
		if !progress {
			break
		}
	}
	r.unresolved(typeErrs)
}

// target finds the overload set a call's function expression names, if it is an
// overloaded name not yet resolved.
func (r *resolver) target(fun ast.Expr) (*overloadSet, *ast.Ident) {
	switch fun := ast.Unparen(fun).(type) {
	case *ast.Ident:
		if s := r.funcs[fun.Name]; s != nil && r.info.Uses[fun] == nil && r.info.Defs[fun] == nil {
			return s, fun
		}
	case *ast.SelectorExpr:
		if !r.methodNames[fun.Sel.Name] || r.info.Selections[fun] != nil {
			return nil, nil
		}
		tv, ok := r.info.Types[fun.X]
		if !ok || tv.Type == nil {
			return nil, nil
		}
		if named := namedOf(tv.Type); named != nil && named.Obj().Pkg() == r.pkg {
			if s := r.methods[named.Obj().Name()+"."+fun.Sel.Name]; s != nil {
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

func (r *resolver) call(f *fileState, call *ast.CallExpr) bool {
	set, id := r.target(call.Fun)
	if set == nil || r.failed[id] {
		return false
	}
	args, ok := r.args(call)
	if !ok {
		return false
	}
	o, err := r.pick(set, args, call.Ellipsis.IsValid())
	if err != "" {
		r.failed[id] = true
		r.errs.add(f.nodePos(call.Pos()), "%s", err)
		return false
	}
	f.rename(id, id.Name, o.mangled, r.errs)
	return true
}

func (r *resolver) args(call *ast.CallExpr) ([]arg, bool) {
	var out []arg
	for _, a := range call.Args {
		tv, ok := r.info.Types[a]
		if !ok || tv.Type == nil || isInvalid(tv.Type) {
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

func isInvalid(t types.Type) bool {
	b, ok := t.(*types.Basic)
	return ok && b.Kind() == types.Invalid
}

// pick chooses the overload that fits best: each argument scores 0 for an
// identical type, 1 for an untyped constant whose default type is the
// parameter's, 2 for any other assignable value. Lowest total wins; a tie is
// ambiguous.
func (r *resolver) pick(set *overloadSet, args []arg, spread bool) (*overload, string) {
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
		return nil, fmt.Sprintf("no overload of %s accepts (%s)%s", set.display(), r.argList(args), r.candidates(set.list))
	}
	return nil, fmt.Sprintf("call of %s with (%s) is ambiguous%s", set.display(), r.argList(args), r.candidates(winners))
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

func (r *resolver) argList(args []arg) string {
	q := types.RelativeTo(r.pkg)
	s := make([]string, len(args))
	for i, a := range args {
		s[i] = types.TypeString(a.t, q)
	}
	return strings.Join(s, ", ")
}

func (r *resolver) candidates(list []*overload) string {
	q := types.RelativeTo(r.pkg)
	var b strings.Builder
	for _, o := range list {
		if o.sig != nil {
			fmt.Fprintf(&b, "\n\t%s at %s", strings.TrimPrefix(types.TypeString(o.sig, q), "func"), o.file.nodePos(o.decl.Name.Pos()))
		}
	}
	return b.String()
}

// unresolved reports every overloaded name still not pointing at an overload.
func (r *resolver) unresolved(typeErrs []types.Error) {
	stuck := false
	for _, f := range r.files {
		if !f.vuka {
			continue
		}
		calls := map[*ast.Ident]bool{}
		ast.Inspect(f.ast, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				if _, id := r.target(n.Fun); id != nil {
					calls[id] = true
				}
			case *ast.SelectorExpr:
				if set, id := r.target(n); set != nil && !r.failed[id] && !calls[id] {
					r.errs.add(f.nodePos(id.Pos()), "%s is overloaded and can't be used as a value; call it, or name one overload with @export", set.display())
				}
				ast.Inspect(n.X, func(m ast.Node) bool { return r.checkIdent(f, m, calls, &stuck) })
				return false
			}
			return r.checkIdent(f, n, calls, &stuck)
		})
	}
	if !stuck {
		return
	}
	shown := 0
	for _, e := range typeErrs {
		if shown == 5 || r.mentionsOverload(e.Msg) {
			continue
		}
		pos := e.Fset.Position(e.Pos)
		if f := r.fileAt(e.Fset, e.Pos); f != nil {
			pos = f.pos(f.tf.Offset(e.Pos))
		}
		r.errs.add(pos, "%s", e.Msg)
		shown++
	}
}

func (r *resolver) checkIdent(f *fileState, n ast.Node, calls map[*ast.Ident]bool, stuck *bool) bool {
	id, ok := n.(*ast.Ident)
	if !ok {
		return true
	}
	set := r.funcs[id.Name]
	if set == nil || r.failed[id] || r.info.Uses[id] != nil || r.info.Defs[id] != nil {
		return true
	}
	if calls[id] {
		*stuck = true
		r.errs.add(f.nodePos(id.Pos()), "can't choose an overload of %s: an argument's type is unknown", set.display())
	} else {
		r.errs.add(f.nodePos(id.Pos()), "%s is overloaded and can't be used as a value; call it, or name one overload with @export", set.display())
	}
	return true
}

func (r *resolver) mentionsOverload(msg string) bool {
	for name := range r.funcs {
		if strings.Contains(msg, "undefined: "+name) {
			return true
		}
	}
	for name := range r.methodNames {
		if strings.Contains(msg, "no field or method "+name) {
			return true
		}
	}
	return false
}

func (r *resolver) fileAt(fset *token.FileSet, p token.Pos) *fileState {
	tf := fset.File(p)
	for _, f := range r.files {
		if f.tf == tf && f.vuka {
			return f
		}
	}
	return nil
}
