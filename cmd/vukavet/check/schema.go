package check

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// Schema checks orm models' Schema methods as the orm does when it maps a
// model: s.Field given something other than the address of a field of the
// receiver (E0501), a nil attribute (E0502), and a field behind a pointer,
// which the zero value Schema runs on doesn't have (E0503).
var Schema = &analysis.Analyzer{
	Name: "schema",
	Doc:  "check orm Schema methods as the orm does when it maps a model",
	URL:  docsURL,
	Run:  runSchema,
}

func runSchema(pass *analysis.Pass) (any, error) {
	info := pass.TypesInfo
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Name.Name != "Schema" || fd.Body == nil || len(fd.Recv.List) != 1 || len(fd.Recv.List[0].Names) != 1 {
				continue
			}
			sig := info.Defs[fd.Name].Type().(*types.Signature)
			if sig.Params().Len() != 1 || !named(sig.Params().At(0).Type(), ormPath, "Schema") {
				continue
			}
			recv := info.Defs[fd.Recv.List[0].Names[0]]
			model := sig.Recv().Type()
			if p, ok := model.(*types.Pointer); ok {
				model = p.Elem()
			}
			name := typeString(pass, model)
			if model == sig.Recv().Type() {
				report(pass, fd.Recv, "E0501", "declare it on the pointer: func (p *"+name+") Schema(s *orm.Schema)", "%s's Schema has a value receiver, so its fields' addresses are a copy's", name)
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch m, _ := method(info, call, ormPath, "Schema"); m {
				case "Model":
					nilAttrs(pass, call, call.Args, "s.Model is given a nil attribute")
				case "Field":
					if len(call.Args) == 0 {
						return true
					}
					field(pass, recv, name, call.Args[0])
					nilAttrs(pass, call, call.Args[1:], "s.Field(%s, …) is given a nil attribute", types.ExprString(call.Args[0]))
				}
				return true
			})
		}
	}
	return nil, nil
}

func nilAttrs(pass *analysis.Pass, call *ast.CallExpr, args []ast.Expr, format string, a ...any) {
	if call.Ellipsis.IsValid() {
		return
	}
	for _, x := range args {
		if tv, ok := pass.TypesInfo.Types[x]; ok && tv.IsNil() {
			report(pass, x, "E0502", "drop it, or pass an attribute", format, a...)
		}
	}
}

// field checks s.Field's first argument: &p.X, p the receiver, through
// the structs the model holds by value.
func field(pass *analysis.Pass, recv types.Object, model string, arg ast.Expr) {
	info := pass.TypesInfo
	help := "pass the address of the receiver's field: s.Field(&" + recv.Name() + ".Title, …)"
	u, ok := ast.Unparen(arg).(*ast.UnaryExpr)
	if !ok || u.Op != token.AND {
		report(pass, arg, "E0501", help, "s.Field is given %s, not the address of one of %s's fields", typeString(pass, info.TypeOf(arg)), model)
		return
	}
	notField := func() {
		report(pass, arg, "E0501", help, "s.Field is given %s, which isn't a field of %s", types.ExprString(arg), model)
	}
	e := ast.Unparen(u.X)
	var steps []*ast.SelectorExpr
	for {
		sel, ok := e.(*ast.SelectorExpr)
		if !ok {
			break
		}
		steps = append(steps, sel)
		e = ast.Unparen(sel.X)
	}
	if star, ok := e.(*ast.StarExpr); ok {
		e = ast.Unparen(star.X)
	}
	id, ok := e.(*ast.Ident)
	if !ok || len(steps) == 0 || info.Uses[id] != recv {
		notField()
		return
	}
	for i := len(steps) - 1; i >= 0; i-- {
		s, ok := info.Selections[steps[i]]
		if !ok || s.Kind() != types.FieldVal {
			notField()
			return
		}
		t := s.Recv()
		if p, ok := t.(*types.Pointer); ok {
			if i != len(steps)-1 {
				report(pass, arg, "E0503", "embed the struct by value, or declare the field's attributes on its own type", "s.Field is given %s, behind the pointer %s: Schema runs on a zero value, where it is nil", types.ExprString(arg), types.ExprString(steps[i].X))
				return
			}
			t = p.Elem()
		}
		idx := s.Index()
		for _, j := range idx[:len(idx)-1] {
			f := t.Underlying().(*types.Struct).Field(j)
			if _, ok := f.Type().(*types.Pointer); ok {
				report(pass, arg, "E0503", "embed the struct by value, or declare the field's attributes on its own type", "s.Field is given %s, behind the embedded pointer %s: Schema runs on a zero value, where it is nil", types.ExprString(arg), f.Name())
				return
			}
			t = f.Type()
		}
	}
}
