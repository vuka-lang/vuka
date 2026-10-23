package transpile

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// The warnings about the context processors' variables a package reads,
// which vuka vet prints: a name shadowing one (W0401), web.Request read in
// markup where request would do (W0402), and a control shown behind
// perms.Has whose event checks no permission (W0403).

// ctxWarnings are the warnings of the package's files, by file.
func (e *engine) ctxWarnings() map[*fileState]ErrorList {
	cv := e.ctx
	if cv == nil || e.info == nil || e.pkg == nil {
		return nil
	}
	out := map[*fileState]ErrorList{}
	for _, f := range e.vuka {
		if f.ast == nil {
			continue
		}
		var list ErrorList
		e.shadowWarnings(f, &list)
		if f.html != nil {
			e.requestWarnings(f, &list)
			e.permWarnings(f, &list)
		}
		if len(list) > 0 {
			out[f] = list
		}
	}
	return out
}

// inMarkup reports whether the node at p is in f's markup.
func (f *fileState) inMarkup(p token.Pos) bool {
	if f.html == nil || f.html.recv == "" || !f.lowerable(p) {
		return false
	}
	off := f.orig(p)
	return off >= f.html.start && off < f.html.end
}

// shadowWarnings reports a field of f's struct, or a local its markup
// declares, named as a processor variable: the template reads it, not the
// variable (W0401).
func (e *engine) shadowWarnings(f *fileState, list *ErrorList) {
	cv := e.ctx
	warn := func(id *ast.Ident, what string) {
		v, _ := cv.lookup(id.Name)
		if v == nil {
			return
		}
		w := list.add("W0401", f.pos(f.off(id.Pos())), "%s %s has the name of the variable %s of the context processor %s: the template reads it, not the variable", what, id.Name, id.Name, v.Processor.Display())
		w.Len = len(id.Name)
		w.help("rename it, or read the variable as web.Var[%s](ctx, %q)", e.display(v.Type), id.Name)
	}
	if h := f.html; h != nil && h.shape != nil && h.recvType != "" {
		for _, d := range f.ast.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, s := range gd.Specs {
				ts := s.(*ast.TypeSpec)
				st, ok := ts.Type.(*ast.StructType)
				if !ok || ts.Name.Name != h.recvType {
					continue
				}
				for _, fl := range st.Fields.List {
					for _, n := range fl.Names {
						if cv.names[n.Name] {
							warn(n, "the field")
						}
					}
				}
			}
		}
	}
	for id, obj := range e.info.Defs {
		if obj == nil || !cv.names[id.Name] || e.fileOf(id.Pos()) != f || !f.inMarkup(id.Pos()) {
			continue
		}
		if v, ok := obj.(*types.Var); ok && !v.IsField() {
			warn(id, "the local")
		}
	}
}

// requestWarnings reports web.Request(ctx) in f's markup where the
// request variable is listed (W0402).
func (e *engine) requestWarnings(f *fileState, list *ErrorList) {
	if v, why := e.ctx.lookup("request"); v == nil || why != "" {
		return
	}
	ast.Inspect(f.ast, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !f.inMarkup(call.Pos()) {
			return true
		}
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok {
			return true
		}
		fn, ok := e.info.Uses[sel.Sel].(*types.Func)
		if !ok || fn.Pkg() == nil || fn.Pkg().Path() != WebPath || fn.Name() != "Request" {
			return true
		}
		w := list.add("W0402", f.pos(f.off(call.Pos())), "the markup reads web.Request(ctx): the request variable has what a template reads of the request, its path, query and parameters")
		w.Len = f.off(call.End()) - f.off(call.Pos())
		w.suggest("request")
		w.help("read request.Path, request.Query or request.Params: a staying layout then renders again only where it reads them")
		return true
	})
}

// permEventAttr is an on… attribute naming a method: onClick={Approve},
// onClick={p.Approve}.
var permEventAttr = regexp.MustCompile(`\bon[A-Z]\w*=\{\s*(?:[A-Za-z_]\w*\.)?([A-Z]\w*)\s*[})(]`)

// permWarnings reports a control in f's markup shown behind
// perms.Has("x") whose event method checks no permission x: hiding it
// protects nothing (W0403).
func (e *engine) permWarnings(f *fileState, list *ErrorList) {
	ast.Inspect(f.ast, func(n ast.Node) bool {
		ifs, ok := n.(*ast.IfStmt)
		if !ok || !f.inMarkup(ifs.Pos()) {
			return true
		}
		perm, ok := e.permCheck(ifs.Cond)
		if !ok {
			return true
		}
		from := f.orig(ifs.Body.Lbrace)
		to := closingBrace(f.src, from)
		if to < 0 {
			return true
		}
		for _, m := range permEventAttr.FindAllSubmatchIndex(f.src[from:to], -1) {
			method := string(f.src[from+m[2] : from+m[3]])
			if e.enforces(f.html.recvType, method, perm) {
				continue
			}
			w := list.add("W0403", f.at(from+m[2]), "%s is shown only with the permission %q, but %s.%s checks none: hiding a control doesn't stop its event being sent", method, perm, f.html.recvType, method)
			w.Len = m[3] - m[2]
			w.help("check it where the event runs: @auth.Requires(%q) on %s, or auth.Require(ctx, %q) in it", perm, method, perm)
		}
		return true
	})
}

// permCheck is the permission a condition tests with perms.Has("x"), the
// auth processor's Perms.
func (e *engine) permCheck(cond ast.Expr) (string, bool) {
	perm, found := "", false
	ast.Inspect(cond, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || found || len(call.Args) != 1 {
			return !found
		}
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Has" {
			return true
		}
		tv, ok := e.info.Types[sel.X]
		if !ok || !isNamed(tv.Type, WebPath+"/auth", "Perms") {
			return true
		}
		if v := e.info.Types[call.Args[0]].Value; v != nil && v.Kind() == constant.String {
			perm, found = constant.StringVal(v), true
		}
		return !found
	})
	return perm, found
}

// enforces reports whether typ's method checks perm: @auth.Requires(perm)
// or @auth.RequiresAny(…, perm, …) on it or on typ, or a call of
// auth.Require or auth.Can with perm in its body. A pattern (orders.*)
// covers what it matches.
func (e *engine) enforces(typ, method, perm string) bool {
	covers := func(args string) bool {
		for _, lit := range stringLits.FindAllString(args, -1) {
			p, err := strconv.Unquote(lit)
			if err != nil {
				continue
			}
			if ok, _ := path.Match(p, perm); ok || p == perm || p == "*" {
				return true
			}
		}
		return false
	}
	for _, f := range e.vuka {
		for _, a := range f.attrs {
			if a.Decl != typ+"."+method && a.Decl != typ {
				continue
			}
			if (strings.HasSuffix(a.Name, ".Requires") || strings.HasSuffix(a.Name, ".RequiresAny")) && covers(a.Args) {
				return true
			}
		}
	}
	for _, f := range e.files {
		if f.ast == nil {
			continue
		}
		for _, d := range f.ast.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Name.Name != method || recvBase(fd) != typ || fd.Body == nil {
				continue
			}
			found := false
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || found {
					return !found
				}
				sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
				if !ok {
					return true
				}
				fn, ok := e.info.Uses[sel.Sel].(*types.Func)
				if !ok || fn.Pkg() == nil || fn.Pkg().Path() != WebPath+"/auth" {
					return true
				}
				switch fn.Name() {
				case "Require", "Can", "RequireAny":
					for _, a := range call.Args {
						if v := e.info.Types[a].Value; v != nil && v.Kind() == constant.String && covers(strconv.Quote(constant.StringVal(v))) {
							found = true
						}
					}
				}
				return !found
			})
			if found {
				return true
			}
		}
	}
	return false
}

var stringLits = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)

// closingBrace is the offset of the } closing the { at off in src, -1 when
// there is none: the body of a markup statement, its markup and holes in
// it.
func closingBrace(src []byte, off int) int {
	if off < 0 || off >= len(src) || src[off] != '{' {
		return -1
	}
	depth := 0
	var quote byte
	for i := off; i < len(src); i++ {
		c := src[i]
		switch {
		case quote != 0:
			if c == '\\' && quote != '`' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '`':
			quote = c
		case c == '{':
			depth++
		case c == '}':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return -1
}
