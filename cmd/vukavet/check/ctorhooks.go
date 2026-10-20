package check

import (
	"go/ast"
	"go/types"
	"strings"
)

// ctorHooks are the hooks a page type's code declares on its embedded
// ui.Live — l.OnMount((*Login).mount), l.Loads("stats", …), l.OnChange(…) —
// as ui keeps them at run time: per type.
type ctorHooks struct {
	mount, onChange string            // the hooked methods
	loads           map[string]string // group (as loaders name it) → method
	hooks           map[string]bool   // the methods hooked
}

// ctorHooks reads every OnMount, Loads and OnChange call of the package,
// reporting a hook that isn't a method expression of the page or doesn't
// fit, a group that isn't a name, a hook declared twice or by both the
// constructor and LiveSchema (E0336), and a hook declared outside a
// function returning the page (W0305).
func (l *liveCheck) ctorHooks() map[*types.Named]*ctorHooks {
	pass, info := l.pass, l.pass.TypesInfo
	out := map[*types.Named]*ctorHooks{}
	eachDecl(pass.Files, func(fn *ast.FuncDecl, decl ast.Node) {
		ast.Inspect(decl, func(x ast.Node) bool {
			call, ok := x.(*ast.CallExpr)
			if !ok {
				return true
			}
			kind, page := liveMethod(info, call)
			if kind == "" || page == nil {
				return true
			}
			name := page.Obj().Name()
			args := call.Args
			what, group := kind, ""
			if kind == "Loads" {
				if len(args) != 2 {
					return true
				}
				g, ok := str(info, args[0])
				if !ok {
					return true
				}
				if !isIdent(g) {
					l.report(args[0], "E0336", `name the group as ui.Defer does: l.Loads("stats", …)`, "%s.Loads(%q, …): a group is a name", name, g)
					return true
				}
				what, group, args = "Loads("+g+")", groupName(g), args[1:]
			}
			if len(args) != 1 {
				return true
			}
			m := l.hookMethod(page, kind, what, args[0])
			if m == "" {
				return true
			}
			if fn == nil || !returns(info, fn, page) {
				l.report(call, "W0305", "declare it in the constructor, a function returning *"+name,
					"%s.%s is declared in %s, which doesn't return the page: a page built elsewhere may not have it", name, what, funcLabel(fn))
			}
			h := out[page]
			if h == nil {
				h = &ctorHooks{loads: map[string]string{}, hooks: map[string]bool{}}
				out[page] = h
			}
			prev := map[string]string{"OnMount": h.mount, "OnChange": h.onChange, "Loads": h.loads[group]}[kind]
			switch {
			case prev == m:
			case prev != "":
				l.report(call, "E0336", "give the hook one method: every constructor declares the same", "%s.%s is declared twice, as %s and as %s", name, what, prev, m)
			default:
				switch kind {
				case "OnMount":
					h.mount = m
				case "OnChange":
					h.onChange = m
				default:
					h.loads[group] = m
				}
				h.hooks[m] = true
			}
			if s := l.schema(page); s != nil {
				twice := kind == "OnMount" && s.mount || kind == "OnChange" && s.onChange || kind == "Loads" && s.loads[group]
				if twice {
					l.report(call, "E0336", "declare the hook in one place", "%s.%s is declared by its constructor and by its LiveSchema", name, what)
				}
			}
			return true
		})
	})
	l.literals(out)
	return out
}

// liveMethod is the ui.Live method a call calls (OnMount, Loads or
// OnChange) and the page type whose Live it is.
func liveMethod(info *types.Info, call *ast.CallExpr) (string, *types.Named) {
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return "", nil
	}
	s, ok := info.Selections[sel]
	if !ok || s.Kind() != types.MethodVal {
		return "", nil
	}
	f, ok := s.Obj().(*types.Func)
	if !ok {
		return "", nil
	}
	recv := f.Type().(*types.Signature).Recv()
	if recv == nil || !named(recv.Type(), uiPath, "Live") {
		return "", nil
	}
	switch f.Name() {
	case "OnMount", "Loads", "OnChange":
	default:
		return "", nil
	}
	t := s.Recv()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, _ := types.Unalias(t).(*types.Named)
	if n == nil || named(n, uiPath, "Live") {
		return "", nil
	}
	return f.Name(), n
}

// hookMethod is the method a hook argument names, "" after reporting one
// that isn't a method expression of page or doesn't fit the hook.
func (l *liveCheck) hookMethod(page *types.Named, kind, what string, arg ast.Expr) string {
	pass, info := l.pass, l.pass.TypesInfo
	name := page.Obj().Name()
	sel, _ := ast.Unparen(arg).(*ast.SelectorExpr)
	var s *types.Selection
	if sel != nil {
		s = info.Selections[sel]
	}
	if s == nil || s.Kind() != types.MethodExpr {
		help := "pass a method expression: l." + kind + "((*" + name + ").mount)"
		if s != nil && s.Kind() == types.MethodVal {
			help = "pass a method expression, (*" + name + ")." + sel.Sel.Name + ": the runtime runs the hook on the page it holds"
		}
		l.report(arg, "E0336", help, "%s.%s is given %s, not a method expression of the page", name, what, types.ExprString(arg))
		return ""
	}
	if n := methodExprRecv(info, arg); n == nil || n.Obj() != page.Obj() {
		l.report(arg, "E0336", "pass a method of "+name, "%s.%s is given a method of another type, %s", name, what, types.ExprString(arg))
		return ""
	}
	if s.Obj().Exported() {
		l.report(arg, "E0336", "hook an unexported method: the browser calls a page's exported methods", "%s.%s is given %s, an exported method", name, what, types.ExprString(arg))
		return ""
	}
	sig := s.Obj().Type().(*types.Signature)
	p, res := sig.Params(), sig.Results()
	okOut := res.Len() == 0 || res.Len() == 1 && types.Identical(res.At(0).Type(), errorType)
	okIn := p.Len() == 0 || p.Len() == 1 && isContext(p.At(0).Type())
	help := "declare it func() or func(ctx context.Context), returning nothing or an error"
	if kind == "OnChange" {
		okIn = changedOK(sig)
		help = "declare it func(), adding a context.Context and a ui.Changed as it needs them, returning nothing or an error"
	}
	if !okIn || !okOut {
		l.report(arg, "E0336", help, "%s.%s is given %s, a %s", name, what, types.ExprString(arg), typeString(pass, sig))
		return ""
	}
	return sel.Sel.Name
}

// literals reports a composite literal of a type whose hooks only its
// constructor declares, made outside a function returning it and outside
// tests (W0304): a page or component built that way has the hooks only
// once a constructor ran.
func (l *liveCheck) literals(hooked map[*types.Named]*ctorHooks) {
	if len(hooked) == 0 {
		return
	}
	pass, info := l.pass, l.pass.TypesInfo
	var files []*ast.File
	for _, f := range pass.Files {
		if !strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			files = append(files, f)
		}
	}
	eachDecl(files, func(fn *ast.FuncDecl, decl ast.Node) {
		ast.Inspect(decl, func(x ast.Node) bool {
			lit, ok := x.(*ast.CompositeLit)
			if !ok {
				return true
			}
			n, _ := types.Unalias(info.TypeOf(lit)).(*types.Named)
			if n == nil || hooked[n] == nil || fn != nil && returns(info, fn, n) {
				return true
			}
			name := n.Obj().Name()
			l.report(lit, "W0304", "build it with its constructor, New"+name+"(…), or declare its hooks in LiveSchema",
				"%s is built as a literal, and its hooks are declared by its constructor: this one has them only once a constructor ran", name)
			return true
		})
	})
}

// eachDecl calls do with each declaration of files, and the function it
// is (nil for another declaration).
func eachDecl(files []*ast.File, do func(fn *ast.FuncDecl, decl ast.Node)) {
	for _, f := range files {
		for _, d := range f.Decls {
			fn, _ := d.(*ast.FuncDecl)
			do(fn, d)
		}
	}
}

// returns reports whether fn returns n or *n.
func returns(info *types.Info, fn *ast.FuncDecl, n *types.Named) bool {
	obj, ok := info.Defs[fn.Name].(*types.Func)
	if !ok {
		return false
	}
	res := obj.Type().(*types.Signature).Results()
	for i := range res.Len() {
		t := res.At(i).Type()
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
		}
		if m, ok := types.Unalias(t).(*types.Named); ok && m.Obj() == n.Obj() {
			return true
		}
	}
	return false
}

func funcLabel(fn *ast.FuncDecl) string {
	if fn == nil {
		return "a package-level declaration"
	}
	return fn.Name.Name
}

// merge adds the constructor's hooks to what the page's LiveSchema says.
func (h *ctorHooks) merge(s *liveSchema) *liveSchema {
	if h == nil {
		return s
	}
	if s == nil {
		s = &liveSchema{fields: map[*types.Var]schemaField{}, loads: map[string]bool{}, hooks: map[string]bool{}}
	}
	for g := range h.loads {
		s.loads[g] = true
	}
	for m := range h.hooks {
		s.hooks[m] = true
	}
	s.mount = s.mount || h.mount != ""
	s.onChange = s.onChange || h.onChange != ""
	return s
}
