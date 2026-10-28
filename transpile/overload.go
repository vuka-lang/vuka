package transpile

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"strings"
)

// An overload is one declaration of an overloaded function or method. It is
// renamed after its parameters' types, as Go would name it — area(c Circle)
// is areaCircle, Update(ctx, m NewMessage) is UpdateNewMessage — and every
// call is pointed at the overload whose parameters fit the arguments'
// static types best.
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

// findOverloads collects the functions and methods declared more than once
// in files, and renames each declaration after its parameters' types: a
// name no declaration of the package (all) has, numbered when two would
// share one.
func findOverloads(files, all []*fileState, errs *ErrorList) map[string]*overloadSet {
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
	taken := declaredNames(all)
	for _, key := range order {
		s := sets[key]
		if len(s.list) < 2 {
			delete(sets, key)
			continue
		}
		seen := map[string]*overload{}
		for _, o := range s.list {
			if o.decl.Type.TypeParams != nil {
				errs.add("E0120", o.file.nodePos(o.decl.Name.Pos()), "generic functions can't be overloaded yet")
			}
			for _, prev := range seen {
				if paramTypes(prev) == paramTypes(o) {
					errs.add("E0120", o.file.nodePos(o.decl.Name.Pos()), "%s is already declared with parameters (%s) at %s",
						s.display(), paramTypes(o), prev.file.nodePos(prev.decl.Name.Pos()))
				}
			}
			base := overloadName(o.decl)
			o.mangled = base
			for n := 2; seen[o.mangled] != nil || taken[s.recv+"."+o.mangled]; n++ {
				o.mangled = base + itoa(n)
			}
			seen[o.mangled] = o
		}
	}
	if len(*errs) == 0 {
		for _, s := range sets {
			for _, o := range s.list {
				o.file.add(o.nameOff, o.nameOff+len(s.name), o.mangled)
				o.file.renameDoc(o.nameOff, o.mangled, s.name)
			}
		}
	}
	return sets
}

// declaredNames are the package's names: "."+name at package level,
// "T."+name for the methods and fields of T.
func declaredNames(files []*fileState) map[string]bool {
	names := map[string]bool{}
	for _, f := range files {
		if f.ast == nil {
			continue
		}
		for _, d := range f.ast.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				names[recvBase(d)+"."+d.Name.Name] = true
			case *ast.GenDecl:
				for _, sp := range d.Specs {
					switch sp := sp.(type) {
					case *ast.TypeSpec:
						names["."+sp.Name.Name] = true
						if st, ok := sp.Type.(*ast.StructType); ok {
							for _, fl := range st.Fields.List {
								for _, n := range fl.Names {
									names[sp.Name.Name+"."+n.Name] = true
								}
								if len(fl.Names) == 0 {
									names[sp.Name.Name+"."+embeddedName(fl.Type)] = true
								}
							}
						}
					case *ast.ValueSpec:
						for _, n := range sp.Names {
							names["."+n.Name] = true
						}
					}
				}
			}
		}
	}
	return names
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

// overloadName names an overload after its parameters' types, the way a
// person writing Go would: area(c Circle) is areaCircle, scale(s *Shape,
// by float64) scaleShapeFloat64, Add(nums ...int) AddInts, an overload
// taking nothing area0. A leading context.Context is left out when more
// follows: Update(ctx, t Typing) is UpdateTyping.
func overloadName(fd *ast.FuncDecl) string {
	var keys []string
	for i, field := range fd.Type.Params.List {
		if i == 0 && types.ExprString(field.Type) == "context.Context" && len(field.Names) <= 1 && len(fd.Type.Params.List) > 1 {
			continue
		}
		k := TypeName(field.Type)
		for range max(1, len(field.Names)) {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return fd.Name.Name + "0"
	}
	return fd.Name.Name + strings.Join(keys, "")
}

// TypeName is a type written as part of a Go name: int is Int, *Shape
// Shape, time.Time Time, []Order Orders, map[string]int MapStringInt, any
// Any. An overload's name is its function's followed by its parameters':
// ui.TypeName is the same for a reflect.Type, and ui.IsUpdate finds an
// Update overload by it.
func TypeName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return upperFirst(t.Name)
	case *ast.ParenExpr:
		return TypeName(t.X)
	case *ast.StarExpr:
		return TypeName(t.X)
	case *ast.SelectorExpr:
		return upperFirst(t.Sel.Name)
	case *ast.Ellipsis:
		return plural(TypeName(t.Elt))
	case *ast.ArrayType:
		return plural(TypeName(t.Elt))
	case *ast.MapType:
		return "Map" + TypeName(t.Key) + TypeName(t.Value)
	case *ast.ChanType:
		return "Chan" + TypeName(t.Value)
	case *ast.FuncType:
		return "Func"
	case *ast.InterfaceType:
		if t.Methods == nil || len(t.Methods.List) == 0 {
			return "Any"
		}
		return "Interface"
	case *ast.StructType:
		return "Struct"
	case *ast.IndexExpr:
		return TypeName(t.X) + TypeName(t.Index)
	case *ast.IndexListExpr:
		k := TypeName(t.X)
		for _, x := range t.Indices {
			k += TypeName(x)
		}
		return k
	}
	if s := sanitize(types.ExprString(e)); s != "" {
		return upperFirst(s)
	}
	return "T"
}

// plural is a name's plural, as Go writes one: Ints, Addresses, Entries.
func plural(s string) string {
	switch {
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "z"),
		strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es"
	case len(s) > 1 && s[len(s)-1] == 'y' && !strings.ContainsRune("aeiouAEIOU", rune(s[len(s)-2])):
		return s[:len(s)-1] + "ies"
	}
	return s + "s"
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
	markup := map[*ast.CallExpr]bool{}
	if f.html != nil && f.partsDone {
		for _, c := range e.markupCalls(f) {
			markup[c] = true
		}
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
		if f.done[overDone(off)] || !f.lowerable(id.Pos()) {
			return true
		}
		args, ok := e.args(call)
		if !ok {
			return true
		}
		o, msg := e.pick(set, args, call.Ellipsis.IsValid())
		withCtx := false
		if o == nil && markup[call] && !f.html.ownCtx {
			// In markup, an overload taking the render's ctx first.
			if c := ctxParam(set); c != nil {
				if o2, msg2 := e.pick(set, append([]arg{{t: c}}, args...), call.Ellipsis.IsValid()); o2 != nil || !strings.HasPrefix(msg2, "no overload") {
					o, msg, withCtx = o2, msg2, o2 != nil
				}
			}
		}
		if withCtx && !f.html.contextual {
			if e.forceCtx == nil {
				e.forceCtx = map[string]bool{}
			}
			e.forceCtx[f.name], e.recompile = true, true
			return false
		}
		f.done[overDone(off)] = true
		if withCtx {
			e.insertCtx(f, call)
		}
		if msg != "" {
			e.errs.add("E0121", f.nodePos(call.Pos()), "%s", msg)
			return true
		}
		f.rename(off, id.Name, o.mangled)
		e.progress = true
		return true
	})
}

// rename writes the name at off as to: into the rewrite qualifying it
// with a template's receiver, when there is one.
func (f *fileState) rename(off int, name, to string) {
	for i, ed := range f.fixed {
		if ed.start == off && ed.end == off+len(name) && len(ed.segs) == 2 && strings.HasSuffix(ed.text, name) {
			q := strings.TrimSuffix(ed.text, name)
			f.fixed[i].text = q + to
			f.fixed[i].segs = []segment{ed.segs[0], {gen: len(q), genLen: len(to), src: off, srcLen: len(name)}}
			return
		}
	}
	f.add(off, off+len(name), to)
}

// overDone is the key in fileState.done of a call resolved to an overload
// at off: apart from the name's own, which a template's receiver may
// qualify.
func overDone(off int) int { return -off - 1 }

// ctxParam is the context.Context an overload of set takes first, if one
// does.
func ctxParam(set *overloadSet) types.Type {
	for _, o := range set.list {
		if o.sig != nil && o.sig.Params().Len() > 0 && isContext(o.sig.Params().At(0).Type()) {
			return o.sig.Params().At(0).Type()
		}
	}
	return nil
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
			if f.done[overDone(f.orig(id.Pos()))] {
				return
			}
			if calls[id] {
				stuck = true
				e.errs.add("E0121", f.nodePos(id.Pos()), "can't choose an overload of %s: an argument's type is unknown", set.display())
			} else {
				e.errs.add("E0121", f.nodePos(id.Pos()), "%s is overloaded and can't be used as a value; call it, or name one overload with @export", set.display())
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
