package transpile

import (
	"bytes"
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
// which vuka vet prints: a name shadowing one (W0401), auth.Can,
// auth.Current or web.Request read where perms, user or request would do
// (W0402), and a control shown behind perms.Has whose event checks no
// permission (W0403).

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
		e.directWarnings(f, &list)
		if f.html != nil {
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

// mayReadDirect reports whether src may call auth.Can, auth.Current or
// web.Request: a file that doesn't needs no types for W0402.
func mayReadDirect(src []byte) bool {
	return bytes.Contains(src, []byte("Can(")) || bytes.Contains(src, []byte("Current(")) || bytes.Contains(src, []byte("Request("))
}

// directRead is a call reading directly what a processor variable gives:
// auth.Can(ctx, "x"), auth.Current(ctx), auth.Current(ctx).Can("x"),
// web.Request(ctx) used through what request has.
type directRead struct {
	expr      ast.Expr // the whole read: web.Request(ctx).URL.Path
	call      string   // auth.Can
	what      string   // the visitor's permissions
	name      string   // the variable: perms
	pkg, typ  string   // its type: auth.Perms
	processor string   // the processor giving it: auth.Context
	fix       string   // the read with the variable: perms.Has("x"); "" when it has no source text
}

// directWarnings reports, anywhere in a vuka file, auth.Can, auth.Current
// and web.Request read where a processor variable gives the same: one
// spelling, tracked in markup (W0402). A read the variable can't give —
// web.Request(ctx).Header — isn't one.
func (e *engine) directWarnings(f *fileState, list *ErrorList) {
	for _, r := range e.directReads(f) {
		start, end := f.orig(r.expr.Pos()), f.orig(r.expr.End())
		want := r.fix
		if want == "" {
			want = r.name
		}
		w := list.add("W0402", f.at(start), "%s reads %s directly: a vuka file reads %s, %s's variable, which markup tracks and an event sees as current", r.call, r.what, r.name, r.processor)
		w.Len = f.at(end).Offset - f.at(start).Offset
		v, why := e.ctx.lookup(r.name)
		switch {
		case v == nil || !isNamed(deref(v.Type), r.pkg, r.typ):
			w.help("list %s in main's web.ContextProcessors(…), then write %s", r.processor, want)
		case why != "":
			w.help("%s; then write %s", why, want)
		case e.shadows(f, r.name, r.expr.Pos()):
			w.help("rename the %s declared here, then write %s", r.name, want)
		case r.fix != "":
			w.Fix = r.fix
			w.help("write %s: vuka fix ctx-vars rewrites it", r.fix)
		default:
			w.help("write %s", want)
		}
	}
}

func deref(t types.Type) types.Type {
	if p, ok := types.Unalias(t).(*types.Pointer); ok {
		return p.Elem()
	}
	return t
}

// shadows reports whether name means something else at pos: a local, a
// package-level name, or in the markup a field or method of its receiver.
func (e *engine) shadows(f *fileState, name string, pos token.Pos) bool {
	if sc := e.pkg.Scope().Innermost(pos); sc != nil {
		if _, obj := sc.LookupParent(name, pos); obj != nil {
			return true
		}
	}
	if !f.inMarkup(pos) {
		return false
	}
	obj, _ := e.pkg.Scope().Lookup(f.html.recvType).(*types.TypeName)
	if obj == nil {
		return false
	}
	o, _, _ := types.LookupFieldOrMethod(types.NewPointer(obj.Type()), true, e.pkg, name)
	return o != nil
}

// directReads are f's reads of what a processor variable gives.
func (e *engine) directReads(f *fileState) []directRead {
	src := func(x ast.Expr) string {
		from, to := f.orig(x.Pos()), f.orig(x.End())
		if from < 0 || to > len(f.src) || from >= to {
			return ""
		}
		return string(f.src[from:to])
	}
	callOf := func(x ast.Expr, pkg, name string) *ast.CallExpr {
		call, ok := ast.Unparen(x).(*ast.CallExpr)
		if !ok || len(call.Args) != 1 && name != "Can" {
			return nil
		}
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != name {
			return nil
		}
		fn, ok := e.info.Uses[sel.Sel].(*types.Func)
		if !ok || fn.Pkg() == nil || fn.Pkg().Path() != pkg || fn.Type().(*types.Signature).Recv() != nil {
			return nil
		}
		return call
	}
	const authPath = WebPath + "/auth"
	// The variable reads the ctx in scope: a read of another context has
	// no rewrite.
	onCtx := func(call *ast.CallExpr) bool { return src(call.Args[0]) == "ctx" }
	perms := func(x ast.Expr, call, arg string, ok bool) directRead {
		r := directRead{expr: x, call: call, what: "the visitor's permissions", name: "perms", pkg: authPath, typ: "Perms", processor: "auth.Context"}
		if arg != "" && ok {
			r.fix = "perms.Has(" + arg + ")"
		}
		return r
	}
	var out []directRead
	done := map[ast.Node]bool{}
	var stack []ast.Node
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		stack = append(stack, n)
		x, ok := n.(ast.Expr)
		if !ok || done[n] || !f.lowerable(x.Pos()) {
			return true
		}
		parent := func(i int) ast.Node { // the i-th node above n
			if len(stack) > i {
				return stack[len(stack)-1-i]
			}
			return nil
		}
		// auth.Current(ctx).Can("x")
		if call, ok := x.(*ast.CallExpr); ok && len(call.Args) == 1 {
			if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && sel.Sel.Name == "Can" {
				if inner := callOf(sel.X, authPath, "Current"); inner != nil {
					done[inner] = true
					out = append(out, perms(call, "auth.Current(ctx).Can", src(call.Args[0]), onCtx(inner)))
					return true
				}
			}
		}
		if call := callOf(x, authPath, "Can"); call != nil && len(call.Args) == 2 {
			out = append(out, perms(call, "auth.Can", src(call.Args[1]), onCtx(call)))
			return true
		}
		if call := callOf(x, authPath, "Current"); call != nil {
			r := directRead{expr: call, call: "auth.Current", what: "the signed-in user", name: "user", pkg: authPath, typ: "Identity", processor: "auth.Context"}
			if onCtx(call) {
				r.fix = "user"
			}
			out = append(out, r)
			return true
		}
		call := callOf(x, WebPath, "Request")
		if call == nil {
			return true
		}
		// web.Request(ctx).URL.Path, .URL.Query(), .PathValue("id"): what
		// request has.
		r := directRead{call: "web.Request", what: "the request", name: "request", pkg: WebPath, typ: "RequestInfo", processor: "web.RequestContext"}
		s1, _ := parent(1).(*ast.SelectorExpr)
		switch {
		case s1 != nil && s1.Sel.Name == "PathValue":
			if c, ok := parent(2).(*ast.CallExpr); ok && c.Fun == s1 && len(c.Args) == 1 {
				r.expr = c
				if arg := src(c.Args[0]); arg != "" {
					r.fix = "request.Params[" + arg + "]"
				}
			}
		case s1 != nil && s1.Sel.Name == "URL":
			s2, _ := parent(2).(*ast.SelectorExpr)
			switch {
			case s2 != nil && s2.Sel.Name == "Path":
				r.expr, r.fix = s2, "request.Path"
			case s2 != nil && s2.Sel.Name == "Query":
				if c, ok := parent(3).(*ast.CallExpr); ok && c.Fun == s2 && len(c.Args) == 0 {
					r.expr, r.fix = c, "request.Query"
				}
			}
		}
		if r.expr != nil {
			if !onCtx(call) {
				r.fix = ""
			}
			out = append(out, r)
		}
		return true
	})
	for i := range out {
		if strings.Contains(out[i].fix, "\n") || out[i].fix != "" && strings.HasSuffix(out[i].fix, "()") {
			out[i].fix = "" // an argument with no source text
		}
	}
	return out
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
			w.help("check it where the event runs: @auth.Requires(%q) on %s, or perms.Has(%q) in it", perm, method, perm)
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
// perms.Has, auth.Can (or auth.Require) with perm in its body. A pattern
// (orders.*) covers what it matches.
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
				name := fn.Name()
				if recv := fn.Type().(*types.Signature).Recv(); name == "Has" && recv != nil && isNamed(recv.Type(), WebPath+"/auth", "Perms") {
					name = "Can" // perms.Has("x"), perms being auth.Context's
				}
				switch name {
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
