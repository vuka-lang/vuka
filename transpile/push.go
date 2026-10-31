package transpile

import (
	"go/ast"
	"go/types"
	"strings"
)

// registerPushes registers each method a Push or Options step of the
// package's markup names (ui.JS.Push(p.Details, id, ui.Read()),
// ui.JS.Options(target, c.CitiesOf, args…)) when the page can call it over
// HTTP — a method of a page or component that isn't live, any method a
// read or an Options step names — and checks each step's arguments and
// Into against its handler's parameters and result:
//
//	func init() {
//		ui.RegisterPush((*Checkout).CitiesOf)
//	}
//
// A handler's parameters are a context.Context (first, optional), the
// step's arguments in order — the parameters that aren't pointers or
// interfaces, one struct at most, which the enclosing form fills when the
// step passes one argument fewer — and dependencies (pointers and
// interfaces). It returns nothing, an error, or a result — []ui.Option,
// []ui.OptionGroup, a ui.Node, a string, number, bool or time.Time, a
// struct, ui.Commands — and maybe an error. It reports whether it wrote
// any registration: the files type-check once more with them.
func (e *engine) registerPushes() bool {
	wrote := false
	for _, f := range e.vuka {
		if f.target == nil || f.ast == nil {
			continue
		}
		into := map[*ast.CallExpr]*ast.CallExpr{} // a Push step → the Into after it
		ast.Inspect(f.ast, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || f.off(call.Pos()) >= f.body {
				return true
			}
			if name, _, ok := e.chainStep(f, call); ok && name == "Into" {
				if p := e.pushBefore(f, call); p != nil {
					into[p] = call
				}
			}
			return true
		})
		seen := map[string]bool{}
		var regs []string
		first := -1
		ast.Inspect(f.ast, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || f.off(call.Pos()) >= f.body {
				return true
			}
			name, on, ok := e.chainStep(f, call)
			if !ok || name != "Push" && name != "Options" {
				return true
			}
			at := 0 // Push(handler, args…), CommandsOn.Options(handler, args…)
			if name == "Options" && !on {
				at = 1 // Commands.Options(target, handler, args…)
			}
			if len(call.Args) <= at {
				return true
			}
			reg, ok := e.pushMethod(f, call, name == "Options", call.Args[at], call.Args[at+1:], into[call])
			if !ok || reg == "" || seen[reg] || name != "Options" && !f.target.has("RegisterPush") {
				return true // a ui from before Push took results registers Options' methods only
			}
			seen[reg] = true
			regs = append(regs, reg)
			if first < 0 {
				first = f.orig(call.Pos())
			}
			return true
		})
		if len(regs) == 0 {
			continue
		}
		register := "RegisterPush"
		if !f.target.has(register) {
			register = "RegisterOptions" // a ui from before Push took results
		}
		if !f.target.has(register) {
			continue // a ui calling no method over HTTP
		}
		var b strings.Builder
		b.WriteString("\nfunc init() {\n")
		for _, r := range regs {
			b.WriteString("\t" + f.target.q + register + "(" + r + ")\n")
		}
		b.WriteString("}\n")
		f.optionsGen.gen(b.String(), first)
		f.makeTrailer(e.bare)
		wrote = true
	}
	return wrote
}

// chainStep is the step call is of a chain of the target's commands — its
// method's name ("Push", "Options", "Into") — and whether it is
// CommandsOn's (its target given by On).
func (e *engine) chainStep(f *fileState, call *ast.CallExpr) (name string, on, ok bool) {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel {
		return "", false, false
	}
	s := e.info.Selections[sel]
	if s == nil || s.Kind() != types.MethodVal {
		return "", false, false
	}
	fn, _ := s.Obj().(*types.Func)
	if fn == nil || fn.Type().(*types.Signature).Recv() == nil {
		return "", false, false
	}
	recv := fn.Type().(*types.Signature).Recv().Type()
	if !declaredBy(f.target, recv) {
		return "", false, false
	}
	if p, isPtr := recv.(*types.Pointer); isPtr {
		recv = p.Elem()
	}
	switch types.Unalias(recv).(*types.Named).Obj().Name() {
	case "Commands":
		return sel.Sel.Name, false, true
	case "CommandsOn":
		return sel.Sel.Name, true, true
	}
	return "", false, false
}

// pushBefore is the Push or Options step an Into applies to: the last
// before it in its chain, past the steps that change a push (OnError,
// OnSuccess, Timeout, another Into).
func (e *engine) pushBefore(f *fileState, into *ast.CallExpr) *ast.CallExpr {
	x := into.Fun.(*ast.SelectorExpr).X
	for {
		call, ok := ast.Unparen(x).(*ast.CallExpr)
		if !ok {
			return nil
		}
		name, _, ok := e.chainStep(f, call)
		switch {
		case !ok:
			return nil
		case name == "Push" || name == "Options":
			return call
		case name == "OnError" || name == "OnSuccess" || name == "Timeout" || name == "Into":
			x = call.Fun.(*ast.SelectorExpr).X
		default:
			return nil
		}
	}
}

// pushArg is one of a step's arguments as it fills a parameter.
type pushArg struct {
	expr  ast.Expr
	value bool // ui.Value's: a string parses as the parameter's type
}

// pushMethod checks a Push or Options step's handler, arguments and Into,
// and is the method expression registering the handler,
// (*Checkout).CitiesOf, when the page calls it over HTTP; "" when it
// doesn't (a live page's push over its socket, a closure).
func (e *engine) pushMethod(f *fileState, call *ast.CallExpr, options bool, h ast.Expr, rest []ast.Expr, into *ast.CallExpr) (string, bool) {
	text := func(n ast.Node) string { return string(f.src[f.orig(n.Pos()):f.orig(n.End())]) }
	step := "ui.JS.Push"
	example := "func (p *Page) M(ctx context.Context, args…, deps…) (ui.Node, error), as p.M"
	if options {
		step = "ui.JS.Options"
		example = "func (p *Page) M(ctx context.Context, args…, deps…) ([]ui.Option, error), as p.M"
	}
	fail := func(n ast.Node, format string, a ...any) (string, bool) {
		e.errs.add("E0168", f.at(f.orig(n.Pos())), format, a...).
			help("pass a method of the page or of a component, %s", example)
		return "", false
	}

	// the arguments, told from the options by their types
	var args []pushArg
	read := options
	for _, a := range rest {
		t := e.info.TypeOf(a)
		switch {
		case t == nil:
			return "", false
		case isTargetNamed(f, t, "IntoOption"):
			return fail(a, "%s goes in Into, after the push: %s(…).Into(target, %s)", text(a), step, text(a))
		case isTargetNamed(f, t, "PushOption"):
			c, _ := ast.Unparen(a).(*ast.CallExpr)
			switch e.pushOptionFunc(f, c) {
			case "Read", "Cache":
				read = true
			case "Value":
				if len(c.Args) == 1 {
					args = append([]pushArg{{c.Args[0], true}}, args...)
				}
			}
		default:
			args = append(args, pushArg{expr: a})
		}
	}
	spread := call.Ellipsis.IsValid()

	// the handler: a method of the package's page or component, else (a
	// push only) any func, checked but not registered
	var sig *types.Signature
	var method *types.Func
	var named *types.Named
	stateful := false
	ptr := false
	if sel, ok := ast.Unparen(h).(*ast.SelectorExpr); ok {
		if s := e.info.Selections[sel]; s != nil && s.Kind() == types.MethodVal {
			method = s.Obj().(*types.Func)
			sig = method.Type().(*types.Signature)
			recv := sig.Recv().Type()
			if p, isPtr := recv.(*types.Pointer); isPtr {
				recv, ptr = p.Elem(), true
			}
			named, _ = types.Unalias(recv).(*types.Named)
			on := s.Recv() // a method promoted to a live page runs on the live page
			if p, isPtr := on.(*types.Pointer); isPtr {
				on = p.Elem()
			}
			if n, _ := types.Unalias(on).(*types.Named); n != nil {
				stateful = e.statefulType(f, n)
			}
			switch {
			case named == nil || named.Obj().Pkg() == nil || e.pkg == nil || named.Obj().Pkg() != e.pkg:
				if options || read {
					return fail(h, "%s is a method of %s, another package's type: %s takes a method of this package's page or component", text(h), e.display(sig.Recv().Type()), step)
				}
				method = nil
			case named.TypeParams().Len() > 0:
				if options || read {
					return fail(h, "%s is a method of a generic type: %s takes a method of a concrete page or component", text(h), step)
				}
				method = nil
			}
		}
	}
	if sig == nil {
		if options || read {
			return fail(h, "%s takes a method of the page or of a component (p.CitiesOf), and %s isn't one", step, text(h))
		}
		t := e.info.TypeOf(h)
		if t == nil {
			return "", false
		}
		fs, ok := t.Underlying().(*types.Signature)
		if !ok {
			return "", false // not a func: the render reports it
		}
		sig = fs
	}
	name := text(h)
	if method != nil {
		name = method.Name()
	}

	// the result, and what Into needs of it
	res := sig.Results()
	errType := types.Universe.Lookup("error").Type()
	kind := ""
	switch {
	case res.Len() == 0:
	case res.Len() == 1 && types.Identical(res.At(0).Type(), errType):
	case res.Len() <= 2 && (res.Len() == 1 || types.Identical(res.At(1).Type(), errType)):
		kind = e.resultKind(f, res.At(0).Type())
		if kind == "" {
			return fail(h, "%s returns %s: a pushed method returns nothing, an error, or a result — []ui.Option, []ui.OptionGroup, a ui.Node, a string, number, bool or time.Time, a struct, ui.Commands — and maybe an error", name, e.display(res.At(0).Type()))
		}
	default:
		return fail(h, "%s returns %s: a pushed method returns nothing, an error, or one result and maybe an error", name, e.display(res))
	}
	if options && kind != "options" {
		return fail(h, "%s returns %s: a method loading options returns []ui.Option or []ui.OptionGroup, and maybe an error", name, e.display(res))
	}
	if into != nil && kind == "" {
		return fail(into, "%s returns no result to put Into a target: Into takes what a handler returns besides an error", name)
	}
	if sig.Variadic() {
		return fail(h, "%s is variadic: a pushed method takes its arguments one by one", name)
	}

	// the parameters the arguments fill
	var params []*types.Var
	form := -1
	ps := sig.Params()
	for i := 0; i < ps.Len(); i++ {
		t := ps.At(i).Type()
		switch {
		case isContext(t) && i == 0:
		case isContext(t):
			return fail(h, "%s takes a context.Context after other parameters: put it first", name)
		case isPointerOrInterface(t):
			// a dependency, injected
		default:
			switch t.Underlying().(type) {
			case *types.Chan, *types.Signature:
				return fail(h, "%s takes a %s, which no argument can be", name, e.display(t))
			}
			if formStruct(t) {
				if form >= 0 {
					return fail(h, "%s takes two structs, %s and %s: a pushed method takes one struct at most, the form it is sent", name, e.display(params[form].Type()), e.display(t))
				}
				form = len(params)
			}
			params = append(params, ps.At(i))
		}
	}
	fromForm := form >= 0 && len(args) == len(params)-1
	switch {
	case spread:
	case len(args) == len(params), fromForm:
	case len(args) == 0 && len(params) == 1:
		// the element's value (or the event's payload, on a live page)
	default:
		want := make([]string, len(params))
		for i, p := range params {
			want[i] = strings.TrimSpace(p.Name() + " " + e.display(p.Type()))
		}
		noun := "arguments"
		if len(params) == 1 {
			noun = "argument"
		}
		why := "its arguments fill the parameters that aren't pointers or interfaces, in order"
		if form >= 0 {
			why += ", or all but its struct, which the enclosing form fills"
		}
		return fail(call, "%s takes %d %s (%s) and the step passes %d: %s", name, len(params), noun, strings.Join(want, ", "), len(args), why)
	}
	if !spread {
		next := 0
		for i, p := range params {
			if fromForm && i == form || next >= len(args) {
				continue
			}
			a := args[next]
			next++
			at := e.info.TypeOf(a.expr)
			if at == nil {
				continue
			}
			pname := strings.TrimSpace(p.Name() + " " + e.display(p.Type()))
			switch {
			case isTargetNamed(f, at, "ValueCondition"):
				if !scalarParam(p.Type()) {
					return fail(a.expr, "%s is an element's value, a string, and %s's %s parameter, %s, takes no string", text(a.expr), name, ordinalWord(i+1), pname)
				}
			case isTargetNamed(f, at, "Condition"):
				if !isBasic(p.Type(), types.IsBoolean) && !isBasic(p.Type(), types.IsString) {
					return fail(a.expr, "%s is read from an element (a bool, or an attribute's text), and %s's %s parameter, %s, takes neither", text(a.expr), name, ordinalWord(i+1), pname)
				}
			case a.value && isBasic(at, types.IsString) && scalarParam(p.Type()):
				// ui.Value's text parses as the parameter's type
			case !types.AssignableTo(at, p.Type()):
				return fail(a.expr, "%s is %s, which %s's %s parameter, %s, can't take", text(a.expr), e.display(at), name, ordinalWord(i+1), pname)
			}
		}
	}
	if method == nil {
		return "", true
	}
	if !stateful {
		e.lostWrites(f, h, method)
	}
	if read && !options {
		e.readWrites(f, h, method)
	}
	if stateful && !read {
		return "", true // over the live page's socket
	}
	tn := named.Obj().Name()
	if ptr {
		return "(*" + tn + ")." + method.Name(), true
	}
	return tn + "." + method.Name(), true
}

// pushOptionFunc is the name of the target's function call calls (ui.Read),
// or "".
func (e *engine) pushOptionFunc(f *fileState, call *ast.CallExpr) string {
	if call == nil {
		return ""
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	fn, _ := e.info.Uses[sel.Sel].(*types.Func)
	if fn == nil || fn.Pkg() == nil || f.target == nil || fn.Pkg().Path() != f.target.path {
		return ""
	}
	return fn.Name()
}

// resultKind is what a handler's result of type t does to the push's
// target: "options", "node", "scalar", "struct", "commands"; "" when it
// is none of them.
func (e *engine) resultKind(f *fileState, t types.Type) string {
	if sl, ok := t.(*types.Slice); ok && (isTargetNamed(f, sl.Elem(), "Option") || isTargetNamed(f, sl.Elem(), "OptionGroup")) {
		return "options"
	}
	if isTargetNamed(f, t, "Commands") || isTargetNamed(f, t, "CommandsOn") {
		return "commands"
	}
	if node, _ := e.targetObj(f.target, "Node").(*types.TypeName); node != nil {
		nt := node.Type()
		if types.Identical(t, nt) {
			return "node"
		}
		if it, ok := nt.Underlying().(*types.Interface); ok && it.NumMethods() > 0 && types.Implements(t, it) {
			return "node"
		}
	}
	if scalarParam(t) || isNamed(t, "time", "Time") {
		return "scalar"
	}
	base := t
	if p, ok := t.(*types.Pointer); ok {
		base = p.Elem()
	}
	if _, ok := base.Underlying().(*types.Struct); ok {
		return "struct"
	}
	return ""
}

// formStruct reports whether a parameter of type t is a struct a form's
// fields fill: a struct that is a group of fields, not one value (a
// time.Time, a TextUnmarshaler).
func formStruct(t types.Type) bool {
	if _, ok := t.Underlying().(*types.Struct); !ok || isNamed(t, "time", "Time") {
		return false
	}
	ms := types.NewMethodSet(types.NewPointer(t))
	return ms.Lookup(nil, "UnmarshalText") == nil
}

// statefulType reports whether n is a live page or stateful component: it
// embeds the target's Live, or its pointer implements the target's Stateful.
func (e *engine) statefulType(f *fileState, n *types.Named) bool {
	if obj, _, _ := types.LookupFieldOrMethod(n, true, nil, "Live"); obj != nil {
		if v, ok := obj.(*types.Var); ok && v.Embedded() && declaredBy(f.target, v.Type()) {
			return true
		}
	}
	iface, _ := e.targetObj(f.target, "Stateful").(*types.TypeName)
	if iface == nil {
		return false
	}
	it, ok := iface.Type().Underlying().(*types.Interface)
	return ok && types.Implements(types.NewPointer(n), it)
}

// methodDecl is the declaration of m in the package, or nil.
func (e *engine) methodDecl(m *types.Func) *ast.FuncDecl {
	for _, f := range e.files {
		if f.ast == nil {
			continue
		}
		for _, d := range f.ast.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil && e.info.Defs[fd.Name] == m {
				return fd
			}
		}
	}
	return nil
}

// lostWrites warns of a stateless page's or component's pushed method
// assigning to its receiver's fields (W0113): it runs on a fresh copy of
// the page, which is thrown away, so the change is lost.
func (e *engine) lostWrites(f *fileState, h ast.Expr, m *types.Func) {
	fd := e.methodDecl(m)
	if fd == nil || fd.Recv == nil || len(fd.Recv.List) == 0 || len(fd.Recv.List[0].Names) == 0 {
		return
	}
	recv := e.info.Defs[fd.Recv.List[0].Names[0]]
	if recv == nil {
		return
	}
	field := ""
	written := func(x ast.Expr) {
		sel, ok := ast.Unparen(x).(*ast.SelectorExpr)
		if !ok || field != "" {
			return
		}
		root := ast.Expr(sel)
		for {
			s, ok := ast.Unparen(root).(*ast.SelectorExpr)
			if !ok {
				break
			}
			root = s.X
		}
		if id, ok := ast.Unparen(root).(*ast.Ident); ok && e.info.Uses[id] == recv {
			field = exprText(sel)
		}
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			for _, l := range s.Lhs {
				written(l)
			}
		case *ast.IncDecStmt:
			written(s.X)
		case *ast.CallExpr:
			// a template file's unexported field, lowered: p.seen.Set(id)
			if sel, ok := ast.Unparen(s.Fun).(*ast.SelectorExpr); ok && (sel.Sel.Name == "Set" || sel.Sel.Name == "Update") {
				if t := e.info.TypeOf(sel.X); t != nil {
					if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Name() == "Assign" {
						written(sel.X)
					}
				}
			}
		}
		return field == ""
	})
	if field == "" {
		return
	}
	f.pushWarns.add("W0113", f.at(f.orig(h.Pos())), "%s assigns %s, but a page that isn't live pushes it over HTTP to a fresh copy of the page, which is thrown away: the change is lost", m.Name(), field).
		help("return what changed (a ui.Node, a value) and put it Into its place, or keep the state where the next request finds it (the database, the session)")
}

// readWrites warns of a read (ui.Read) naming a method that writes
// (W0114): one calling an ORM's Save, Create or Delete, as far as its own
// body shows. A read is a GET, which the app's CSRF protection doesn't
// check, and Cache lets a browser answer it without asking.
func (e *engine) readWrites(f *fileState, h ast.Expr, m *types.Func) {
	fd := e.methodDecl(m)
	if fd == nil {
		return
	}
	write := ""
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || write != "" {
			return write == ""
		}
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch sel.Sel.Name {
		case "Save", "Create", "Delete":
		default:
			return true
		}
		fn, _ := e.info.Uses[sel.Sel].(*types.Func)
		if fn != nil && fn.Pkg() != nil && ormPackage(fn.Pkg()) {
			write = sel.Sel.Name
		}
		return true
	})
	if write == "" {
		return
	}
	f.pushWarns.add("W0114", f.at(f.orig(h.Pos())), "%s calls %s, but the push is a read (ui.Read): a GET, not checked against cross-site requests and maybe answered from the browser's cache", m.Name(), write).
		help("drop ui.Read (and ui.Cache): a push that writes is a POST carrying the CSRF token")
}

// exprText is a selector's text, x.y.z.
func exprText(x ast.Expr) string {
	switch x := x.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprText(x.X) + "." + x.Sel.Name
	case *ast.ParenExpr:
		return exprText(x.X)
	}
	return "…"
}

// isTargetNamed reports whether t is f's JSX target's type of that name.
func isTargetNamed(f *fileState, t types.Type, name string) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Name() == name && declaredBy(f.target, t)
}

func isPointerOrInterface(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Pointer, *types.Interface:
		return true
	}
	return false
}

func isBasic(t types.Type, info types.BasicInfo) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&info != 0
}

// scalarParam reports whether a parameter of type t takes an element's
// value, parsed: a string, number or bool, or a TextUnmarshaler.
func scalarParam(t types.Type) bool {
	if isBasic(t, types.IsString|types.IsNumeric|types.IsBoolean) {
		return true
	}
	ms := types.NewMethodSet(types.NewPointer(t))
	return ms.Lookup(nil, "UnmarshalText") != nil
}

// ordinalWord is n as a word: "1st", "2nd".
func ordinalWord(n int) string {
	switch {
	case n%100 >= 11 && n%100 <= 13:
		return itoa(n) + "th"
	case n%10 == 1:
		return itoa(n) + "st"
	case n%10 == 2:
		return itoa(n) + "nd"
	case n%10 == 3:
		return itoa(n) + "rd"
	}
	return itoa(n) + "th"
}
