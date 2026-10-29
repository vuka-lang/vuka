package transpile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strconv"
	"strings"
)

// failBranch is what a failed ? does: return the error (as ?(…) says it)
// to the caller, or in a test fail it.
func (e *engine) failBranch(f *fileState, t *try, at ast.Node, k tryKind, failVar string) (*gtext, string) {
	sig, _ := e.enclosingFunc(f, at)
	if sig == nil {
		return nil, "? needs an enclosing function"
	}
	fe, msg := e.failValue(f, t, at, k, failVar)
	if msg != "" {
		return nil, msg
	}
	if fe == nil {
		if kind, arg := runtimeType(single(sig.Results())); kind == "Option" {
			s, msg := e.typeText(f, arg)
			g := &gtext{}
			g.s("return " + f.runtime() + ".None[" + s + "]()")
			return g, msg
		}
		msg := "? on an Option needs the function to return an Option"
		if e.returnsError(sig) || testParam(sig) != "" {
			msg += "; write opt?(err) to say which error None is"
		}
		return nil, msg
	}
	g, msg := e.failReturn(f, sig, fe)
	if msg == "" {
		return g, ""
	}
	if name := testParam(sig); name != "" {
		g := &gtext{}
		g.s(name + ".Helper(); " + name + ".Fatal(")
		g.add(fe)
		g.s(")")
		return g, ""
	}
	return nil, msg
}

func (e *engine) returnsError(sig *types.Signature) bool {
	res := sig.Results()
	if n := res.Len(); n > 0 && types.Identical(res.At(n-1).Type(), errorType) {
		return true
	}
	kind, _ := runtimeType(single(res))
	return kind == "Result"
}

// testParam is the name of sig's *testing.T, *testing.B, *testing.F or
// testing.TB parameter, "" for none.
func testParam(sig *types.Signature) string {
	for i := 0; i < sig.Params().Len(); i++ {
		p := sig.Params().At(i)
		t := p.Type()
		if ptr, ok := t.(*types.Pointer); ok {
			t = ptr.Elem()
		}
		n, ok := types.Unalias(t).(*types.Named)
		if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != "testing" {
			continue
		}
		switch n.Obj().Name() {
		case "T", "B", "F", "TB":
			if p.Name() != "" && p.Name() != "_" {
				return p.Name()
			}
		}
	}
	return ""
}

// failValue is the error a failed ? passes on: the operand's own, or what
// ?(…) makes of it. nil is a bare ? on an Option: None.
func (e *engine) failValue(f *fileState, t *try, at ast.Node, k tryKind, failVar string) (*gtext, string) {
	g := &gtext{}
	if t.args.start < 0 {
		if k.option {
			return nil, ""
		}
		g.s(failVar)
		return g, ""
	}
	args, msg := f.tryArgs(t)
	if msg != "" {
		return nil, msg
	}
	if len(args) == 0 {
		return nil, `?() needs what to say on failure: ?("…"), ?(ErrX), ?(wrap) or ?(Enum.Variant)`
	}
	text := func(a span) string { return string(f.src[a.start:a.end]) }
	fmtq := f.importName("fmt")
	rest := func() {
		for _, a := range args[1:] {
			g.s(", ")
			g.c(a.start, a.end)
		}
	}
	if lit := text(args[0]); strings.HasPrefix(lit, `"`) || strings.HasPrefix(lit, "`") {
		if k.option {
			if len(args) == 1 {
				g.s(f.importName("errors") + ".New(" + lit + ")")
				return g, ""
			}
			g.s(fmtq + ".Errorf(" + lit)
			rest()
			g.s(")")
			return g, ""
		}
		g.s(fmtq + ".Errorf(" + lit[:len(lit)-1] + ": %w" + lit[len(lit)-1:])
		rest()
		g.s(", " + failVar + ")")
		return g, ""
	}
	if len(args) == 1 {
		if d, v := e.enumVariantOf(text(args[0])); v != nil {
			if !v.fields {
				// A value variant: an error value of its own.
				return e.errorValue(f, g, args[0], k, failVar, fmtq), ""
			}
			field, msg := e.causeField(d, v)
			if msg != "" {
				return nil, msg
			}
			if k.option {
				return nil, d.name + "." + v.name + " wraps an error, and None has none: give ? an error, ?(ErrX)"
			}
			g.s(d.name + v.name + "{" + field + ": " + failVar + "}")
			return g, ""
		}
	}
	typ, msg := e.argType(f, at, text(args[0]))
	if msg != "" {
		return nil, msg
	}
	switch {
	case isString(typ):
		if k.option {
			if len(args) == 1 {
				g.s(f.importName("errors") + ".New(")
				g.c(args[0].start, args[0].end)
				g.s(")")
				return g, ""
			}
			g.s(fmtq + ".Errorf(")
			g.c(args[0].start, args[0].end)
			rest()
			g.s(")")
			return g, ""
		}
		if len(args) == 1 {
			g.s(fmtq + `.Errorf("%s: %w", `)
			g.c(args[0].start, args[0].end)
			g.s(", " + failVar + ")")
			return g, ""
		}
		g.s(fmtq + ".Errorf(")
		g.c(args[0].start, args[0].end)
		g.s(`+": %w"`)
		rest()
		g.s(", " + failVar + ")")
		return g, ""
	case len(args) > 1:
		return nil, "?(format, args…) needs a format string first; " + text(args[0]) + " is " + e.display(typ)
	case isErrorMapper(typ):
		if k.option {
			return nil, "None has no error for " + text(args[0]) + " to wrap: give ? an error, ?(ErrX)"
		}
		g.c(args[0].start, args[0].end)
		g.s("(" + failVar + ")")
		return g, ""
	case types.Implements(typ, errorType.Underlying().(*types.Interface)):
		return e.errorValue(f, g, args[0], k, failVar, fmtq), ""
	}
	return nil, "?(…) takes a message, a func(error) error, an error or an error enum's variant; " + text(args[0]) + " is " + e.display(typ)
}

// errorValue is ?(ErrX): None becomes ErrX; an error is wrapped in it,
// both matched by errors.Is.
func (e *engine) errorValue(f *fileState, g *gtext, a span, k tryKind, failVar, fmtq string) *gtext {
	if k.option {
		g.c(a.start, a.end)
		return g
	}
	g.s(fmtq + `.Errorf("%w: %w", `)
	g.c(a.start, a.end)
	g.s(", " + failVar + ")")
	return g
}

func isString(t types.Type) bool {
	b, ok := t.Underlying().(*types.Basic)
	return ok && b.Info()&types.IsString != 0
}

func isErrorMapper(t types.Type) bool {
	sig, ok := t.Underlying().(*types.Signature)
	return ok && sig.Params().Len() == 1 && sig.Results().Len() == 1 && !sig.Variadic() &&
		types.Identical(sig.Params().At(0).Type(), errorType) && types.Identical(sig.Results().At(0).Type(), errorType)
}

// tryArgs are the spans of ?(…)'s arguments, split at top-level commas.
func (f *fileState) tryArgs(t *try) ([]span, string) {
	inner := string(f.src[t.args.start+1 : t.args.end])
	if strings.TrimSpace(inner) == "" {
		return nil, ""
	}
	x, err := parser.ParseExpr("_(" + inner + ")")
	call, ok := x.(*ast.CallExpr)
	if err != nil || !ok || call.Ellipsis.IsValid() {
		return nil, "?(…) holds what to say on failure, as a call's arguments do"
	}
	base := t.args.start + 1 - len("_(") - 1
	var out []span
	for _, a := range call.Args {
		out = append(out, span{base + int(a.Pos()), base + int(a.End())})
	}
	return out, ""
}

// argType is the type of the expression text where at is.
func (e *engine) argType(f *fileState, at ast.Node, text string) (types.Type, string) {
	x, err := parser.ParseExprFrom(e.fset, "", text, 0)
	if err == nil {
		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}}
		if err = types.CheckExpr(e.fset, e.pkg, at.Pos(), x, info); err == nil {
			if tv := info.Types[x]; tv.IsValue() {
				return tv.Type, ""
			}
		}
	}
	return nil, "can't tell what ?(" + text + ") says on failure: give it a message, a func(error) error, an error or an error enum's variant"
}

// enumVariantOf is the package's enum variant text names, as Enum.Variant.
func (e *engine) enumVariantOf(text string) (*enumDecl, *enumVariant) {
	enum, variant, ok := strings.Cut(strings.TrimSpace(text), ".")
	if !ok || !token.IsIdentifier(enum) || !token.IsIdentifier(variant) {
		return nil, nil
	}
	for _, f := range e.vuka {
		for _, d := range f.enums {
			if d.name != enum {
				continue
			}
			for _, v := range d.variants {
				if v.name == variant {
					return d, v
				}
			}
		}
	}
	return nil, nil
}

// causeField is the field of v holding the error a ? converts into it: its
// only field, of type error.
func (e *engine) causeField(d *enumDecl, v *enumVariant) (string, string) {
	if fl := e.variantFields(d, v); fl != nil && len(fl.List) == 1 && len(fl.List[0].Names) == 1 {
		if id, ok := fl.List[0].Type.(*ast.Ident); ok && id.Name == "error" {
			return fl.List[0].Names[0].Name, ""
		}
	}
	return "", d.name + "." + v.name + " converts an error in ?(…) when its only field is an error: " + d.name + "." + v.name + "{Err error}"
}

// variantFields are v's fields as written.
func (e *engine) variantFields(d *enumDecl, v *enumVariant) *ast.FieldList {
	for _, f := range e.vuka {
		for _, fd := range f.enums {
			if fd != d {
				continue
			}
			from := v.off + len(v.name)
			toks := scanFrom(f.src, from, false)
			if len(toks) == 0 || toks[0].tok != token.LBRACE {
				return nil
			}
			_, end, ok := matchClose(toks, 0)
			if !ok {
				return nil
			}
			x, err := parser.ParseExpr("struct" + string(f.src[toks[0].off:end]))
			if st, ok := x.(*ast.StructType); err == nil && ok {
				return st.Fields
			}
		}
	}
	return nil
}

// importName is how f names the package at path, importing it when f
// doesn't.
func (f *fileState) importName(path string) string {
	for _, imp := range f.ast.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == path {
			switch {
			case imp.Name == nil:
				return ImportName(path)
			case imp.Name.Name != "_" && imp.Name.Name != ".":
				return imp.Name.Name
			}
		}
	}
	return f.importAs(path, "__"+ImportName(path))
}

// lowerMain runs a main holding a ? through main_() error, so that its
// defers run before the program exits with the error:
//
//	func main() { if err := main_(); err != nil { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) } }
//	func main_() error { …; return nil }
//
// It reports whether it rewrote main this round; the ? is lowered in
// main_ the next.
func (e *engine) lowerMain(f *fileState, stmt ast.Stmt) bool {
	var fd *ast.FuncDecl
	for p := f.parents[stmt]; p != nil && fd == nil; p = f.parents[p] {
		switch p := p.(type) {
		case *ast.FuncLit:
			return false
		case *ast.FuncDecl:
			fd = p
		}
	}
	if fd == nil || fd.Name.Name != "main" || fd.Recv != nil || f.ast.Name.Name != "main" || fd.Body == nil ||
		fd.Type.TypeParams != nil || fd.Type.Params.NumFields() > 0 || fd.Type.Results.NumFields() > 0 {
		return false
	}
	if f.mainDone {
		return true // rewritten this round
	}
	f.mainDone = true
	fmtq, osq := f.importName("fmt"), f.importName("os")
	f.insert(f.orig(fd.Pos()), "func main() { if err := main_(); err != nil { "+fmtq+`.Fprintln(`+osq+`.Stderr, "error:", err); `+osq+".Exit(1) } }; ", f.nextPrio())
	name := f.orig(fd.Name.Pos())
	f.add(name, name+len("main"), "main_")
	f.insert(f.origEnd(fd.Type.Params.End()), " error", 0)
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.ReturnStmt:
			if len(n.Results) == 0 {
				start := f.orig(n.Pos())
				f.add(start, start+len("return"), "return nil")
			}
		}
		return true
	})
	if len(fd.Body.List) == 0 || !e.terminates(fd.Body.List[len(fd.Body.List)-1]) {
		rb := f.orig(fd.Body.Rbrace)
		text := "return nil; "
		if prev := strings.TrimRight(string(f.src[:rb]), " \t"); !strings.HasSuffix(prev, "\n") && !strings.HasSuffix(prev, "{") {
			text = "; " + text
		}
		f.insert(rb, text, f.nextPrio())
	}
	e.progress = true
	return true
}

// terminates reports whether s is a terminating statement, as the Go
// specification defines one; a for or switch with a break is not.
func (e *engine) terminates(s ast.Stmt) bool {
	switch s := s.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return s.Tok == token.GOTO
	case *ast.ExprStmt:
		return e.isPanic(s)
	case *ast.BlockStmt:
		return len(s.List) > 0 && e.terminates(s.List[len(s.List)-1])
	case *ast.IfStmt:
		return s.Else != nil && e.terminates(s.Body) && e.terminates(s.Else)
	case *ast.LabeledStmt:
		return e.terminates(s.Stmt)
	case *ast.ForStmt:
		return s.Cond == nil && !hasBreak(s.Body)
	case *ast.SwitchStmt:
		return clausesTerminate(e, s.Body, true) && !hasBreak(s.Body)
	case *ast.TypeSwitchStmt:
		return clausesTerminate(e, s.Body, true) && !hasBreak(s.Body)
	case *ast.SelectStmt:
		return clausesTerminate(e, s.Body, false) && !hasBreak(s.Body)
	}
	return false
}

func (e *engine) isPanic(s *ast.ExprStmt) bool {
	c, ok := s.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	id, ok := ast.Unparen(c.Fun).(*ast.Ident)
	if !ok || id.Name != "panic" {
		return false
	}
	_, builtin := e.info.Uses[id].(*types.Builtin)
	return builtin
}

func clausesTerminate(e *engine, body *ast.BlockStmt, needDefault bool) bool {
	def := false
	for _, c := range body.List {
		var list []ast.Stmt
		switch c := c.(type) {
		case *ast.CaseClause:
			def = def || c.List == nil
			list = c.Body
		case *ast.CommClause:
			list = c.Body
		}
		if len(list) == 0 {
			return false
		}
		last := list[len(list)-1]
		if b, ok := last.(*ast.BranchStmt); ok && b.Tok == token.FALLTHROUGH {
			continue
		}
		if !e.terminates(last) {
			return false
		}
	}
	return def || !needDefault
}

// hasBreak reports whether body breaks out of the statement holding it: an
// unlabeled break outside nested loops, switches and selects, or any
// labeled one.
func hasBreak(body *ast.BlockStmt) bool {
	found := false
	var walk func(n ast.Node, nested bool)
	walk = func(n ast.Node, nested bool) {
		ast.Inspect(n, func(m ast.Node) bool {
			if found || m == nil {
				return false
			}
			switch m := m.(type) {
			case *ast.FuncLit:
				return false
			case *ast.BranchStmt:
				if m.Tok == token.BREAK && (m.Label != nil || !nested) {
					found = true
				}
			case *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
				if m != n {
					walk(m, true)
					return false
				}
			}
			return true
		})
	}
	walk(body, false)
	return found
}
