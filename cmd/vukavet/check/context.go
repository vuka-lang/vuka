package check

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
)

// Context checks an app's context processors as its boot does: the list a
// package main gives web.ContextProcessors — each entry a function
// func(ctx, deps…) (S, error) (E0425), no two giving one variable (E0421),
// none reading the signed-in user exported to the browser unless marked
// web.Personal (E0426) — and every web.Var read of the packages main links
// in against that list (E0427).
var Context = &analysis.Analyzer{
	Name:      "context",
	Doc:       "check an app's context processors and the web.Var reads of the packages it links in",
	URL:       docsURL,
	Run:       runContext,
	FactTypes: []analysis.Fact{new(readsAuthFact), new(varReadsFact)},
}

const authPath = webPath + "/auth"

// readsAuthFact marks a function reading the signed-in user: it calls into
// web/auth, directly or not.
type readsAuthFact struct{}

func (*readsAuthFact) AFact()         {}
func (*readsAuthFact) String() string { return "readsAuth" }

// varReadsFact is a package's web.Var reads, for the main linking it in.
type varReadsFact struct{ Reads []varRead }

type varRead struct {
	Name, Type, Pos string
}

func (*varReadsFact) AFact()           {}
func (f *varReadsFact) String() string { return fmt.Sprint(f.Reads) }

func runContext(pass *analysis.Pass) (any, error) {
	markAuthReaders(pass)
	reads := ownVarReads(pass)
	if len(reads) > 0 && pass.Pkg.Name() != "main" {
		pass.ExportPackageFact(&varReadsFact{reads})
	}
	if pass.Pkg.Name() != "main" {
		return nil, nil
	}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if ok && funcOf(pass.TypesInfo, call, webPath) == "ContextProcessors" {
				checkProcessors(pass, call, reads)
			}
			return true
		})
	}
	return nil, nil
}

// markAuthReaders exports readsAuthFact for the package's functions that
// read the signed-in user.
func markAuthReaders(pass *analysis.Pass) {
	decls := map[*types.Func]*ast.FuncDecl{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				if fn, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func); ok {
					decls[fn] = fd
				}
			}
		}
	}
	reads := map[*types.Func]bool{}
	for changed := true; changed; {
		changed = false
		for fn, fd := range decls {
			if reads[fn] {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				id, ok := n.(*ast.Ident)
				if !ok || reads[fn] {
					return !reads[fn]
				}
				o := pass.TypesInfo.Uses[id]
				if o == nil || o.Pkg() == nil {
					return true
				}
				callee, isFunc := o.(*types.Func)
				switch {
				case o.Pkg().Path() == authPath:
					reads[fn] = true
				case isFunc && callee.Pkg() == pass.Pkg && reads[callee]:
					reads[fn] = true
				case isFunc && callee.Pkg() != pass.Pkg && pass.ImportObjectFact(callee, new(readsAuthFact)):
					reads[fn] = true
				}
				return !reads[fn]
			})
			changed = changed || reads[fn]
		}
	}
	for fn := range reads {
		pass.ExportObjectFact(fn, &readsAuthFact{})
	}
}

// ownVarReads are the package's web.Var[T](ctx, "name") calls.
func ownVarReads(pass *analysis.Pass) []varRead {
	var out []varRead
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 2 || funcOf(pass.TypesInfo, call, webPath) != "Var" {
				return true
			}
			name, ok := str(pass.TypesInfo, call.Args[1])
			if !ok {
				return true
			}
			t := pass.TypesInfo.TypeOf(call)
			if t == nil {
				return true
			}
			pos := pass.Fset.Position(call.Pos())
			out = append(out, varRead{Name: name, Type: types.TypeString(t, nil), Pos: pos.String()})
			return true
		})
	}
	return out
}

// procVar is a variable a listed processor gives.
type procVar struct {
	name string
	typ  types.Type
	fn   *types.Func
	at   ast.Expr
}

// checkProcessors checks one web.ContextProcessors call and the reads of
// the packages main links in against it.
func checkProcessors(pass *analysis.Pass, call *ast.CallExpr, own []varRead) {
	vars := map[string]*procVar{}
	for _, arg := range call.Args {
		x, browser, personal := unwrap(pass, arg)
		fn := processorFunc(pass, x)
		if fn == nil {
			report(pass, arg, "E0425", "a context processor is func(ctx context.Context, deps…) (S, error), S a struct whose exported fields are its variables",
				"%s isn't a function: list a context processor by its name, users.Context", types.ExprString(arg))
			continue
		}
		st, why := resultStruct(fn)
		if why != "" {
			report(pass, arg, "E0425", "a context processor is func(ctx context.Context, deps…) (S, error), S a struct whose exported fields are its variables",
				"%s isn't a context processor: %s", display(fn), why)
			continue
		}
		if browser && !personal && readsAuth(pass, fn) {
			report(pass, arg, "E0426", "list it as web.Browser(web.Personal("+types.ExprString(x)+")) if it is meant for the signed-in visitor's own browser; never export a raw permission list",
				"%s reads the signed-in user, and web.Browser sends its variables to the browser: mark it web.Personal to say they are the visitor's own", display(fn))
		}
		var clash []string
		var with *procVar
		for i := 0; i < st.NumFields(); i++ {
			fl := st.Field(i)
			if !fl.Exported() || fl.Embedded() {
				continue
			}
			v := &procVar{name: lowerCamel(fl.Name()), typ: fl.Type(), fn: fn, at: arg}
			if prev := vars[v.name]; prev != nil {
				if with == nil || with.fn == prev.fn {
					with, clash = prev, append(clash, v.name)
				}
				continue
			}
			vars[v.name] = v
		}
		if with != nil && with.fn == fn {
			report(pass, arg, "E0421", "list it once", "%s is listed twice", display(fn))
		} else if with != nil {
			report(pass, arg, "E0421", "rename the fields of one result: a template reads each variable by its name",
				"%s (%s) and %s (%s) both provide %s", display(with.fn), declFile(pass, with.fn), display(fn), declFile(pass, fn), strings.Join(clash, ", "))
		}
	}
	reads := append([]varRead(nil), own...)
	for _, pf := range pass.AllPackageFacts() {
		if f, ok := pf.Fact.(*varReadsFact); ok {
			reads = append(reads, f.Reads...)
		}
	}
	names := make([]string, 0, len(vars))
	for n := range vars {
		names = append(names, n)
	}
	sort.Strings(names)
	seen := map[string]bool{}
	for _, r := range reads {
		if seen[r.Pos+r.Name] {
			continue
		}
		seen[r.Pos+r.Name] = true
		v := vars[r.Name]
		switch {
		case v == nil:
			help := "list the context processor giving it in web.ContextProcessors"
			if s := nearest(r.Name, names); s != "" {
				help = "did you mean " + s + "?"
			}
			report(pass, call, "E0427", help, "%s: web.Var reads %q, which no context processor in this list gives", r.Pos, r.Name)
		case types.TypeString(v.typ, nil) != r.Type:
			report(pass, call, "E0427", "read it as web.Var["+types.TypeString(v.typ, (*types.Package).Name)+"]",
				"%s: web.Var reads %q as %s, but %s gives it as %s", r.Pos, r.Name, r.Type, display(v.fn), types.TypeString(v.typ, nil))
		}
	}
}

// unwrap is a list entry without its web.Browser, web.Personal and
// web.Auth wrappers, and which of the first two it had.
func unwrap(pass *analysis.Pass, x ast.Expr) (ast.Expr, bool, bool) {
	browser, personal := false, false
	for {
		call, ok := ast.Unparen(x).(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return x, browser, personal
		}
		switch funcOf(pass.TypesInfo, call, webPath) {
		case "Browser":
			browser = true
		case "Personal":
			personal = true
		case "Auth":
		default:
			return x, browser, personal
		}
		x = call.Args[0]
	}
}

// processorFunc is the function an entry names.
func processorFunc(pass *analysis.Pass, x ast.Expr) *types.Func {
	var id *ast.Ident
	switch x := ast.Unparen(x).(type) {
	case *ast.Ident:
		id = x
	case *ast.SelectorExpr:
		id = x.Sel
	default:
		return nil
	}
	fn, _ := pass.TypesInfo.Uses[id].(*types.Func)
	return fn
}

// resultStruct is the struct a processor returns, or why it isn't one.
func resultStruct(fn *types.Func) (*types.Struct, string) {
	sig := fn.Type().(*types.Signature)
	if sig.Recv() != nil {
		return nil, "it is a method"
	}
	if sig.Params().Len() == 0 || !named(sig.Params().At(0).Type(), "context", "Context") {
		return nil, "its first parameter isn't a context.Context"
	}
	if sig.Results().Len() != 2 || !types.Identical(sig.Results().At(1).Type(), types.Universe.Lookup("error").Type()) {
		return nil, "it doesn't return (S, error)"
	}
	st, ok := sig.Results().At(0).Type().Underlying().(*types.Struct)
	if !ok {
		return nil, "what it returns isn't a struct"
	}
	return st, ""
}

// readsAuth reports whether fn reads the signed-in user.
func readsAuth(pass *analysis.Pass, fn *types.Func) bool {
	if fn.Pkg() != nil && fn.Pkg().Path() == authPath {
		return true
	}
	return pass.ImportObjectFact(fn, new(readsAuthFact))
}

// display is fn as main writes it: users.Context.
func display(fn *types.Func) string {
	if fn.Pkg() == nil {
		return fn.Name()
	}
	return fn.Pkg().Name() + "." + fn.Name()
}

// declFile is the file fn is declared in, by its base name.
func declFile(pass *analysis.Pass, fn *types.Func) string {
	if !fn.Pos().IsValid() {
		return "?"
	}
	p := pass.Fset.Position(fn.Pos())
	if i := strings.LastIndexAny(p.Filename, `/\`); i >= 0 {
		return p.Filename[i+1:] + ":" + fmt.Sprint(p.Line)
	}
	return p.Filename
}

// lowerCamel is a field's variable name: its leading capitals lowered,
// the last of several kept when a lower-case letter follows.
func lowerCamel(name string) string {
	rs := []rune(name)
	n := 0
	for n < len(rs) && unicode.IsUpper(rs[n]) {
		n++
	}
	if n > 1 && n < len(rs) && unicode.IsLower(rs[n]) {
		n--
	}
	for i := 0; i < n; i++ {
		rs[i] = unicode.ToLower(rs[i])
	}
	return string(rs)
}

// nearest is the name of names closest to s, if any is near.
func nearest(s string, names []string) string {
	best, bestD := "", 3
	for _, n := range names {
		if d := distance(s, n); d < bestD && d < len(n) {
			best, bestD = n, d
		}
	}
	return best
}

func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			c := 1
			if a[i-1] == b[j-1] {
				c = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+c)
		}
		prev = cur
	}
	return prev[len(b)]
}
