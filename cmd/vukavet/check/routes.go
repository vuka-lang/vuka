package check

import (
	"fmt"
	"go/ast"
	"go/types"
	"net/http"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Routes checks registrations with web — web.Routes.X(…) and app.X(…) —
// as the app's Build does: a method and path declared twice (E0401), a
// handler that isn't a function (E0403), a scalar parameter with no name
// and no path segment to bind (E0405), results the app can't write
// (E0406), a constructor of the wrong shape (E0418) and a page backed by
// its island under another method than GET (E0331); and an inject: struct
// tag, which no longer says anything (E0420).
var Routes = &analysis.Analyzer{
	Name:      "routes",
	Doc:       "check vuka routes, handlers and constructors as the app's boot does",
	URL:       docsURL,
	Run:       runRoutes,
	FactTypes: []analysis.Fact{new(routesFact)},
}

// routesFact is the routes a package declares on web.Routes, for the
// packages importing it.
type routesFact struct{ Routes []factRoute }

type factRoute struct{ Key, Where string }

func (*routesFact) AFact()           {}
func (f *routesFact) String() string { return fmt.Sprint(f.Routes) }

type route struct {
	key   string
	where string
	reg   registration
}

func runRoutes(pass *analysis.Pass) (any, error) {
	checkInjectTags(pass)
	recorded := recordedParams(pass)
	regs := registrations(pass)
	checkPages(pass, regs)
	checkLayouts(pass)
	prefixes := map[*types.TypeName]string{}
	for _, r := range regs {
		if r.verb != "Controller" {
			continue
		}
		if n := checkCtor(pass, r, fmt.Sprintf("Controller(%q, …)", r.path), "the controller's constructor: func(deps…) *Controller"); n != nil {
			prefixes[n.Obj()] = r.path
		}
	}
	var routes []route
	for _, r := range regs {
		switch r.verb {
		case "Controller":
			continue
		case "Service":
			checkCtor(pass, r, "Service(…)", "the service's constructor: func(deps…) *Service")
			continue
		case "Live", "Page":
		default:
			checkHandler(pass, r, recorded)
		}
		if r.method == "" || r.path == "" {
			continue
		}
		path := r.path
		if recv := methodExprRecv(pass.TypesInfo, r.fn); recv != nil {
			prefix, ok := prefixes[recv.Obj()]
			if !ok {
				continue
			}
			path = joinPath(prefix, path)
		}
		pos := pass.Fset.Position(r.at.Pos())
		where := fmt.Sprintf("%s at %s:%d", types.ExprString(r.fn), filepath.Base(pos.Filename), pos.Line)
		routes = append(routes, route{r.method + " " + path, where, r})
	}

	imported := map[string]string{}
	for _, pf := range pass.AllPackageFacts() {
		if f, ok := pf.Fact.(*routesFact); ok {
			for _, r := range f.Routes {
				imported[r.Key] = pf.Package.Name() + "." + r.Where
			}
		}
	}
	var own []factRoute
	for i, r := range routes {
		if r.reg.global {
			own = append(own, factRoute{r.key, r.where})
		}
		if prev, ok := imported[r.key]; ok {
			report(pass, r.reg.at, "E0401", "give one of them another path or method", "%s is declared twice: by %s and by %s", r.key, prev, r.where)
			continue
		}
		for _, p := range routes[:i] {
			if p.key == r.key && (p.reg.global || r.reg.global || p.reg.app != nil && p.reg.app == r.reg.app) {
				report(pass, r.reg.at, "E0401", "give one of them another path or method", "%s is declared twice: by %s and by %s", r.key, p.where, r.where)
				break
			}
		}
	}
	if len(own) > 0 {
		pass.ExportPackageFact(&routesFact{own})
	}
	return nil, nil
}

// methodExprRecv is the named receiver of a method expression,
// (*Admin).Index, else nil.
func methodExprRecv(info *types.Info, e ast.Expr) *types.Named {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	s, ok := info.Selections[sel]
	if !ok || s.Kind() != types.MethodExpr {
		return nil
	}
	t := s.Recv()
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, _ := types.Unalias(t).(*types.Named)
	return n
}

// checkCtor checks a service's or controller's constructor, the struct it
// builds when it is one.
func checkCtor(pass *analysis.Pass, r registration, what, want string) *types.Named {
	t := pass.TypesInfo.TypeOf(r.fn)
	if t == nil || types.IsInterface(t) {
		return nil
	}
	sig, ok := t.Underlying().(*types.Signature)
	if !ok {
		report(pass, r.fn, "E0418", "pass "+want, "%s: %s is not a function", what, typeString(pass, t))
		return nil
	}
	n, why := ctor(sig, qualifier(pass))
	if n == nil {
		report(pass, r.fn, "E0418", "pass "+want, "%s: %s", what, why)
	}
	return n
}

func checkHandler(pass *analysis.Pass, r registration, recorded map[types.Object][]string) {
	info := pass.TypesInfo
	t := info.TypeOf(r.fn)
	if t == nil || types.IsInterface(t) {
		return
	}
	what := r.method + " " + r.path
	sig, ok := t.Underlying().(*types.Signature)
	if !ok {
		report(pass, r.fn, "E0403", "pass a function", "%s: the handler is %s, not a function", what, typeString(pass, t))
		return
	}
	if r.island(info) {
		if n, _ := ctor(sig, qualifier(pass)); n != nil {
			if r.method != http.MethodGet {
				report(pass, r.at, "E0331", "route the page with Get or Live", "%s (page %s): a page backed by a component is a GET", what, n.Obj().Name())
			}
			return
		}
	}
	if why := resultsWhy(sig.Results()); why != "" {
		report(pass, r.fn, "E0406", "a handler returns nothing, error, T, (T, error) or vuka.Result[T]", "%s (%s): %s", what, types.ExprString(r.fn), why)
	}
	params := sig.Params()
	skip := 0
	if methodExprRecv(info, r.fn) != nil {
		skip = 1
	}
	// A scalar binds by the name the compiler recorded (vuka.RegisterParams);
	// unnamed, the path's segments in order, the last scalar the last
	// segment.
	names := recorded[funcObj(info, r.fn)]
	var unnamed []int
	for i := range params.Len() - skip {
		if basicScalar(params.At(i+skip).Type()) && (i >= len(names) || names[i] == "" || names[i] == "_") {
			unnamed = append(unnamed, i)
		}
	}
	for _, i := range unnamed[min(len(unnamed), len(pathSegments(r.path))):] {
		report(pass, r.fn, "E0405", "take the query in a struct, func(q Query), or add the segment to the path",
			"%s (%s): parameter #%d %s binds the path segment its position gives, and the path has none left for it",
			what, types.ExprString(r.fn), i+1, typeString(pass, params.At(i+skip).Type()))
	}
}

// recordedParams are the parameter names the package records with
// vuka.RegisterParams, as generated code does, by function.
func recordedParams(pass *analysis.Pass) map[types.Object][]string {
	out := map[types.Object][]string{}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok || funcOf(pass.TypesInfo, c, vukaPath) != "RegisterParams" || len(c.Args) == 0 || c.Ellipsis.IsValid() {
				return true
			}
			var names []string
			for _, a := range c.Args[1:] {
				s, ok := str(pass.TypesInfo, a)
				if !ok {
					return true
				}
				names = append(names, s)
			}
			if obj := funcObj(pass.TypesInfo, c.Args[0]); obj != nil {
				out[obj] = names
			}
			return true
		})
	}
	return out
}

// funcObj is the function or method an expression names: F, pkg.F,
// (*T).M; nil for anything else.
func funcObj(info *types.Info, e ast.Expr) types.Object {
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		if f, ok := info.Uses[x].(*types.Func); ok {
			return f
		}
	case *ast.SelectorExpr:
		if s, ok := info.Selections[x]; ok {
			return s.Obj()
		}
		if f, ok := info.Uses[x.Sel].(*types.Func); ok {
			return f
		}
	}
	return nil
}

// pathSegments are a route path's parameters: {id}, {rest...}.
func pathSegments(path string) []string {
	var out []string
	for path != "" {
		i := strings.IndexByte(path, '{')
		if i < 0 {
			break
		}
		j := strings.IndexByte(path[i:], '}')
		if j < 0 {
			break
		}
		out = append(out, strings.TrimSuffix(path[i+1:i+j], "..."))
		path = path[i+j+1:]
	}
	return out
}

// checkInjectTags reports a struct field's inject: tag (E0420): the
// constructor Vuka writes takes a struct's pointer and interface fields,
// and services are registered with app.Service, so the tag says nothing.
func checkInjectTags(pass *analysis.Pass) {
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range st.Fields.List {
				if field.Tag == nil {
					continue
				}
				tag, err := strconv.Unquote(field.Tag.Value)
				if err != nil {
					continue
				}
				if _, ok := reflect.StructTag(tag).Lookup("inject"); ok {
					report(pass, field.Tag, "E0420", "drop the tag: the constructor Vuka writes takes the struct's pointer and interface fields, and nothing else; a struct needing more writes its own New",
						"the inject: tag is gone")
				}
			}
			return true
		})
	}
}

// resultsWhy is why a handler's results can't be written, "" when they can.
func resultsWhy(res *types.Tuple) string {
	switch {
	case res.Len() <= 1:
		return ""
	case res.Len() == 2 && types.Identical(res.At(1).Type(), errorType) && !isResult(res.At(0).Type()):
		return ""
	}
	return "results " + res.String()
}

func isResult(t types.Type) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == vukaPath && n.Obj().Name() == "Result"
}

// basicScalar reports whether t is a scalar a handler binds by name and no
// provider can stand for: an unnamed string, bool or number, a pointer to
// one or a slice of them.
func basicScalar(t types.Type) bool {
	switch x := t.(type) {
	case *types.Basic:
		return x.Info()&(types.IsString|types.IsBoolean|types.IsInteger|types.IsFloat) != 0 && x.Info()&types.IsUntyped == 0 && x.Kind() != types.Uintptr
	case *types.Pointer:
		_, ok := x.Elem().(*types.Basic)
		return ok && basicScalar(x.Elem())
	case *types.Slice:
		_, ok := x.Elem().(*types.Basic)
		return ok && basicScalar(x.Elem())
	}
	return false
}
