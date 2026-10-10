package main

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"

	"github.com/vuka-lang/vuka/internal/load"
)

// The Go templ generates for a .templ file of a module requiring ui is
// instrumented to record live render trees (ui.Rec): on a live page a templ
// component's markup splits into statics and dynamics as JSX's does, so a
// layout or view diffs instead of travelling as one string. Outside a live
// render the recorder only writes, and the component writes the same bytes
// as templ's own code.
func init() { load.TemplInstrument = instrumentTempl }

const (
	recPkg = "templ_vk"   // the import name of ui/templx in instrumented code
	recVar = "templ_vk_r" // a component's recorder
	bufVar = "templ_7745c5c3_Buffer"
)

// instrumentTempl rewrites templ's generated Go so each component records
// its render: after its preamble it takes its recorder (templx.Record), and
// then
//
//	templruntime.WriteString(buf, n, s)  →  r.S(buf, n, s)       a static
//	(after expression output)            +  r.D()                 its end, a dynamic
//	x.Render(ctx, buf)                   →  r.Render(x, ctx, buf) a component: a frame
//	if / switch writing markup           →  r.Open() … r.Close()  a branch's frame
//	for writing markup                   →  r.For() … r.Item() … r.End()
//
// Children blocks ({ children... } content, a call's body) are components
// of their own. Instrumenting instrumented code changes nothing.
func instrumentTempl(src []byte) ([]byte, error) {
	if bytes.Contains(src, []byte(recPkg+".Record(")) {
		return src, nil
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isSel(call.Fun, "templruntime", "GeneratedTemplate") || len(call.Args) != 1 {
			return true
		}
		if fn, ok := call.Args[0].(*ast.FuncLit); ok && instrumentBody(fn.Body) {
			found = true
		}
		return true
	})
	if !found {
		return src, nil
	}
	var buf bytes.Buffer
	if err := format.Node(&buf, fset, file); err != nil {
		return nil, err
	}
	out, err := addImport(buf.Bytes(), recPkg, load.UIPath+"/templx")
	if err != nil {
		return nil, err
	}
	return format.Source(out)
}

// instrumentBody declares the recorder after a component's preamble (ctx =
// templ.InitializeContext(ctx)) and instruments what follows.
func instrumentBody(body *ast.BlockStmt) bool {
	for i, st := range body.List {
		as, ok := st.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 {
			continue
		}
		if call, ok := as.Rhs[0].(*ast.CallExpr); ok && isSel(call.Fun, "templ", "InitializeContext") {
			decl := &ast.AssignStmt{
				Lhs: []ast.Expr{ast.NewIdent(recVar)},
				Tok: token.DEFINE,
				Rhs: []ast.Expr{&ast.CallExpr{Fun: sel(recPkg, "Record"), Args: []ast.Expr{ast.NewIdent("ctx"), ast.NewIdent(bufVar)}}},
			}
			use := &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent("_")}, Tok: token.ASSIGN, Rhs: []ast.Expr{ast.NewIdent(recVar)}}
			rest := recStmts(body.List[i+1:])
			body.List = append(append(append([]ast.Stmt{}, body.List[:i+1]...), decl, use), rest...)
			return true
		}
	}
	return false
}

// recStmts instruments a statement list. A run of statements writing
// expression output (text, attribute values, spread attributes, CSS and
// script items) ends with D before the next static, frame or return, and at
// the end of the list, so the frame's shape is the same whatever they wrote.
func recStmts(list []ast.Stmt) []ast.Stmt {
	out := make([]ast.Stmt, 0, len(list)+4)
	pending := false
	end := func() {
		if pending {
			out = append(out, recCall("D"))
			pending = false
		}
	}
	for _, st := range list {
		if isStatic(st) {
			end()
			out = append(out, st)
			continue
		}
		if _, ok := st.(*ast.ReturnStmt); ok {
			end()
			out = append(out, st)
			continue
		}
		if !writes(st) {
			out = append(out, st)
			continue
		}
		switch s := st.(type) {
		case *ast.RangeStmt:
			end()
			s.Body.List = append([]ast.Stmt{recCall("Item")}, recStmts(s.Body.List)...)
			out = append(out, recCall("For"), s, recCall("End"))
		case *ast.ForStmt:
			end()
			s.Body.List = append([]ast.Stmt{recCall("Item")}, recStmts(s.Body.List)...)
			out = append(out, recCall("For"), s, recCall("End"))
		case *ast.IfStmt:
			end()
			recBranches(s)
			out = append(out, recCall("Open"), s, recCall("Close"))
		case *ast.SwitchStmt:
			end()
			recCases(s.Body)
			out = append(out, recCall("Open"), s, recCall("Close"))
		case *ast.TypeSwitchStmt:
			end()
			recCases(s.Body)
			out = append(out, recCall("Open"), s, recCall("Close"))
		case *ast.BlockStmt:
			end()
			s.List = recStmts(s.List)
			out = append(out, s)
		default:
			if renderCall(st) {
				end()
				out = append(out, st)
				continue
			}
			out = append(out, st)
			pending = true
		}
	}
	end()
	return out
}

func recBranches(s *ast.IfStmt) {
	s.Body.List = recStmts(s.Body.List)
	switch e := s.Else.(type) {
	case *ast.BlockStmt:
		e.List = recStmts(e.List)
	case *ast.IfStmt:
		recBranches(e)
	}
}

func recCases(body *ast.BlockStmt) {
	for _, c := range body.List {
		if cc, ok := c.(*ast.CaseClause); ok {
			cc.Body = recStmts(cc.Body)
		}
	}
}

// isStatic reports whether st is templ_7745c5c3_Err = templruntime.WriteString(buf, n, s),
// rewriting it to the recorder's S.
func isStatic(st ast.Stmt) bool {
	as, ok := st.(*ast.AssignStmt)
	if !ok || len(as.Rhs) != 1 {
		return false
	}
	call, ok := as.Rhs[0].(*ast.CallExpr)
	if !ok || len(call.Args) != 3 || !isIdent(call.Args[0], bufVar) {
		return false
	}
	if isSel(call.Fun, recVar, "S") {
		return true
	}
	if !isSel(call.Fun, "templruntime", "WriteString") {
		return false
	}
	call.Fun = sel(recVar, "S")
	return true
}

// renderCall reports whether st is templ_7745c5c3_Err = x.Render(ctx, buf),
// rewriting it to r.Render(x, ctx, buf). x is evaluated first, as before.
func renderCall(st ast.Stmt) bool {
	as, ok := st.(*ast.AssignStmt)
	if !ok || len(as.Rhs) != 1 {
		return false
	}
	call, ok := as.Rhs[0].(*ast.CallExpr)
	if !ok || len(call.Args) != 2 || !isIdent(call.Args[1], bufVar) {
		return false
	}
	fn, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || fn.Sel.Name != "Render" || writes(&ast.ExprStmt{X: fn.X}) {
		return false
	}
	call.Fun = sel(recVar, "Render")
	call.Args = append([]ast.Expr{fn.X}, call.Args...)
	return true
}

// writes reports whether st uses the component's buffer, outside nested
// components (function literals), which record for themselves.
func writes(st ast.Stmt) bool {
	found := false
	ast.Inspect(st, func(n ast.Node) bool {
		if found {
			return false
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		if isIdent(n, bufVar) {
			found = true
		}
		return true
	})
	return found
}

func recCall(method string) ast.Stmt {
	return &ast.ExprStmt{X: &ast.CallExpr{Fun: sel(recVar, method)}}
}

func sel(x, name string) *ast.SelectorExpr {
	return &ast.SelectorExpr{X: ast.NewIdent(x), Sel: ast.NewIdent(name)}
}

func isSel(e ast.Expr, x, name string) bool {
	s, ok := e.(*ast.SelectorExpr)
	return ok && s.Sel.Name == name && isIdent(s.X, x)
}

func isIdent(n ast.Node, name string) bool {
	id, ok := n.(*ast.Ident)
	return ok && id.Name == name
}

// addImport adds import name "path" after the imports of src, a
// declaration of its own as templ writes each of its own. (Inserted in the
// text: a declaration without a position would take the comments after it.)
func addImport(src []byte, name, path string) ([]byte, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return nil, err
	}
	at := fset.Position(f.Name.End()).Offset
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			at = fset.Position(g.End()).Offset
		}
	}
	line := "\nimport " + name + " " + strconv.Quote(path)
	return append(append(append([]byte{}, src[:at]...), line...), src[at:]...), nil
}
