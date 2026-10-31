package transpile

import (
	"go/ast"
	"go/types"
	"strings"
)

// registerOptions registers each method an Options command of the
// package's markup names (ui.JS.Options(target, c.CitiesOf, args…)), so
// the page rendering it can call it over HTTP, and checks the command's
// arguments against the method's parameters:
//
//	func init() {
//		ui.RegisterOptions((*Checkout).CitiesOf)
//	}
//
// The method returns []ui.Option or []ui.OptionGroup, and maybe an error;
// its parameters are a context.Context (first, optional), the command's
// arguments in order — the parameters that aren't pointers or interfaces
// — and dependencies (pointers and interfaces). It reports whether it
// wrote any registration: the files type-check once more with them.
func (e *engine) registerOptions() bool {
	wrote := false
	for _, f := range e.vuka {
		if f.target == nil || f.ast == nil {
			continue
		}
		seen := map[string]bool{}
		var regs []string
		first := -1
		ast.Inspect(f.ast, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || f.off(call.Pos()) >= f.body {
				return true
			}
			on, ok := e.optionsCall(f, call)
			if !ok {
				return true
			}
			at := 1 // Commands.Options(target, handler, args…)
			if on {
				at = 0 // CommandsOn.Options(handler, args…)
			}
			if len(call.Args) <= at {
				return true
			}
			reg, ok := e.optionsMethod(f, call, call.Args[at], call.Args[at+1:])
			if !ok || seen[reg] {
				return true
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
		var b strings.Builder
		b.WriteString("\nfunc init() {\n")
		for _, r := range regs {
			b.WriteString("\t" + f.target.q + "RegisterOptions(" + r + ")\n")
		}
		b.WriteString("}\n")
		f.optionsGen.gen(b.String(), first)
		f.makeTrailer(e.bare)
		wrote = true
	}
	return wrote
}

// optionsCall reports whether call is a chain's Options step, and whether
// it is CommandsOn's (its target given by On).
func (e *engine) optionsCall(f *fileState, call *ast.CallExpr) (on, ok bool) {
	sel, isSel := call.Fun.(*ast.SelectorExpr)
	if !isSel || sel.Sel.Name != "Options" {
		return false, false
	}
	s := e.info.Selections[sel]
	if s == nil || s.Kind() != types.MethodVal {
		return false, false
	}
	fn, _ := s.Obj().(*types.Func)
	if fn == nil || fn.Type().(*types.Signature).Recv() == nil {
		return false, false
	}
	recv := fn.Type().(*types.Signature).Recv().Type()
	if !declaredBy(f.target, recv) {
		return false, false
	}
	if p, isPtr := recv.(*types.Pointer); isPtr {
		recv = p.Elem()
	}
	switch types.Unalias(recv).(*types.Named).Obj().Name() {
	case "Commands":
		return false, true
	case "CommandsOn":
		return true, true
	}
	return false, false
}

// optionsMethod checks an Options step's handler and arguments, and is the
// method expression registering the handler: (*Checkout).CitiesOf.
func (e *engine) optionsMethod(f *fileState, call *ast.CallExpr, h ast.Expr, args []ast.Expr) (string, bool) {
	text := func(n ast.Node) string { return string(f.src[f.orig(n.Pos()):f.orig(n.End())]) }
	fail := func(n ast.Node, format string, a ...any) (string, bool) {
		e.errs.add("E0168", f.at(f.orig(n.Pos())), format, a...).
			help("pass a method of the page or of a component, func (p *Page) M(ctx context.Context, args…, deps…) ([]ui.Option, error), as p.M")
		return "", false
	}
	sel, ok := ast.Unparen(h).(*ast.SelectorExpr)
	s := e.info.Selections[sel]
	if !ok || s == nil || s.Kind() != types.MethodVal {
		return fail(h, "ui.JS.Options takes a method of the page or of a component (p.CitiesOf), and %s isn't one", text(h))
	}
	fn := s.Obj().(*types.Func)
	sig := fn.Type().(*types.Signature)
	recv := sig.Recv().Type()
	ptr := false
	if p, isPtr := recv.(*types.Pointer); isPtr {
		recv, ptr = p.Elem(), true
	}
	named, _ := types.Unalias(recv).(*types.Named)
	switch {
	case named == nil || named.Obj().Pkg() == nil || e.pkg == nil || named.Obj().Pkg() != e.pkg:
		return fail(h, "%s is a method of %s, another package's type: ui.JS.Options takes a method of this package's page or component", text(h), e.display(sig.Recv().Type()))
	case named.TypeParams().Len() > 0:
		return fail(h, "%s is a method of a generic type: ui.JS.Options takes a method of a concrete page or component", text(h))
	}
	name := fn.Name()
	res := sig.Results()
	listOK := func(t types.Type) bool {
		sl, isSlice := t.Underlying().(*types.Slice)
		return isSlice && types.Identical(t, sl) && (isTargetNamed(f, sl.Elem(), "Option") || isTargetNamed(f, sl.Elem(), "OptionGroup"))
	}
	errType := types.Universe.Lookup("error").Type()
	if res.Len() == 0 || res.Len() > 2 || !listOK(res.At(0).Type()) || res.Len() == 2 && !types.Identical(res.At(1).Type(), errType) {
		return fail(h, "%s returns %s: a method loading options returns []ui.Option or []ui.OptionGroup, and maybe an error", name, e.display(res))
	}
	if sig.Variadic() {
		return fail(h, "%s is variadic: a method loading options takes its arguments one by one", name)
	}
	var params []*types.Var
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
			params = append(params, ps.At(i))
		}
	}
	spread := call.Ellipsis.IsValid()
	if !spread && len(args) != len(params) {
		want := make([]string, len(params))
		for i, p := range params {
			want[i] = strings.TrimSpace(p.Name() + " " + e.display(p.Type()))
		}
		noun := "arguments"
		if len(params) == 1 {
			noun = "argument"
		}
		return fail(call, "%s takes %d %s (%s) and the command passes %d: its arguments fill the parameters that aren't pointers or interfaces, in order",
			name, len(params), noun, strings.Join(want, ", "), len(args))
	}
	for i, a := range args {
		if spread || i >= len(params) {
			break
		}
		p := params[i]
		at := e.info.TypeOf(a)
		if at == nil {
			continue
		}
		pname := strings.TrimSpace(p.Name() + " " + e.display(p.Type()))
		switch {
		case isTargetNamed(f, at, "ValueCondition"):
			if !scalarParam(p.Type()) {
				return fail(a, "%s is an element's value, a string, and %s's %s parameter, %s, takes no string", text(a), name, ordinalWord(i+1), pname)
			}
		case isTargetNamed(f, at, "Condition"):
			if !isBasic(p.Type(), types.IsBoolean) && !isBasic(p.Type(), types.IsString) {
				return fail(a, "%s is read from an element (a bool, or an attribute's text), and %s's %s parameter, %s, takes neither", text(a), name, ordinalWord(i+1), pname)
			}
		case !types.AssignableTo(at, p.Type()):
			return fail(a, "%s is %s, which %s's %s parameter, %s, can't take", text(a), e.display(at), name, ordinalWord(i+1), pname)
		}
	}
	tn := named.Obj().Name()
	if ptr {
		return "(*" + tn + ")." + name, true
	}
	return tn + "." + name, true
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
