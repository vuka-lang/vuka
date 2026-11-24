package main

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"slices"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// Links: the route("name", args…) and routePath calls of a package's Vuka
// files, recorded with its facts as they compile — web.RouteURL and
// web.RoutePath — and checked by the assembler against the route names:
// an unknown name is E0725, arguments the path doesn't take E0726, a query
// key its page doesn't keep E0727. A link's checks follow the routes it
// names without the package transpiling again.

// linkFact is a link of the package's Vuka source.
type linkFact struct {
	Name string
	Fn   string    // route or routePath
	File string    // its source file
	Line int       // about where it is: its generated Go's line
	Nth  int       // it is the file's nth link naming Name, from 0
	Args []argFact // its path arguments
	Keys []string  // its query literal's keys, when it has one
}

// argFact is an argument a link gives a path parameter.
type argFact struct {
	Type   string
	Kind   paramKind
	KindOK bool
}

// mayLink reports whether a package's Vuka sources may link by route name.
func mayLink(p *load.Package) bool {
	for _, f := range p.Files {
		if f.IsVuka() && (strings.Contains(string(f.Src), "route(") || strings.Contains(string(f.Src), "routePath(")) {
			return true
		}
	}
	return false
}

// linksOf are the links of ty, a Vuka package type-checked.
func linksOf(ty *load.Typed) []linkFact {
	var out []linkFact
	nth := map[[2]string]int{}
	for _, file := range ty.Files {
		ast.Inspect(file, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := c.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			fn, ok := ty.Uses[sel.Sel].(*types.Func)
			if !ok || fn.Pkg() == nil || fn.Pkg().Path() != webPath || fn.Name() != "RouteURL" && fn.Name() != "RoutePath" || len(c.Args) < 2 {
				return true
			}
			pos := ty.Fset.Position(c.Pos())
			if !strings.HasSuffix(pos.Filename, ".vuka") && !strings.HasSuffix(pos.Filename, ".vuka.html") {
				return true
			}
			tv := ty.Types[c.Args[1]]
			if tv.Value == nil || tv.Value.Kind() != constant.String {
				return true
			}
			l := linkFact{Name: constant.StringVal(tv.Value), Fn: "route", File: pos.Filename, Line: pos.Line}
			if fn.Name() == "RoutePath" {
				l.Fn = "routePath"
			}
			key := [2]string{l.File, l.Name}
			l.Nth = nth[key]
			nth[key]++
			for i, a := range c.Args[2:] {
				t := ty.Types[a].Type
				if t == nil {
					continue
				}
				if i == len(c.Args)-3 && isNamed(t, webPath, "RouteQuery", false) {
					l.Keys = []string{}
					if cl, ok := a.(*ast.CompositeLit); ok {
						for _, e := range cl.Elts {
							if kv, ok := e.(*ast.KeyValueExpr); ok {
								if v := ty.Types[kv.Key].Value; v != nil && v.Kind() == constant.String {
									l.Keys = append(l.Keys, constant.StringVal(v))
								}
							}
						}
					}
					continue
				}
				af := argFact{Type: types.TypeString(types.Default(t), nil)}
				af.Kind, af.KindOK = paramKindOf(types.Default(t))
				l.Args = append(l.Args, af)
			}
			out = append(out, l)
			return true
		})
	}
	return out
}

// checkLinks checks the links of the tree's packages, and of the packages
// outside pages/ when it is the module's, against its route names.
func (c *routeChecker) checkLinks() {
	var paths []string
	for p, f := range c.t.facts {
		if len(f.Links) > 0 {
			paths = append(paths, p)
		}
	}
	if c.t.ownsLinks {
		for p := range c.t.links {
			paths = append(paths, p)
		}
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	for _, p := range paths {
		var links []linkFact
		if f := c.t.facts[p]; f != nil {
			links = f.Links
		} else if c.t.ownsLinks {
			links = c.t.links[p]
		}
		for _, l := range links {
			c.checkLink(l)
		}
	}
}

func (c *routeChecker) checkLink(l linkFact) {
	for _, pr := range linkProblems(c.t.named, l, true) {
		c.reportAt(pr.d, c.linkSpan(l, pr.key))
	}
}

// linkProblem is a diagnostic of a link: at its name, or at its query's
// key'th key.
type linkProblem struct {
	d   *diag.Diagnostic
	key int
}

// linkProblems are what is wrong with the link l given the route names;
// typed, its arguments' types are checked too.
func linkProblems(named map[string]*namedRoute, l linkFact, typed bool) []linkProblem {
	var out []linkProblem
	nr := named[l.Name]
	if nr == nil {
		names := slices.Sorted(func(yield func(string) bool) {
			for n := range named {
				if !yield(n) {
					return
				}
			}
		})
		d := diag.New("E0725", "%s(%q): no route is named %s", l.Fn, l.Name, l.Name)
		if s := closest(l.Name, names); s != "" {
			d = d.WithHelp("a route is named by its folders under pages/, with dots: %s is %s", s, bracketPath(named[s].Path)).WithSuggest(s)
		} else {
			d = d.WithHelp("a route is named by its folders under pages/, with dots: pages/users/[user]/edit is users.edit (vuka routes lists the paths)")
		}
		return append(out, linkProblem{d, -1})
	}
	if len(l.Args) != len(nr.Params) {
		d := diag.New("E0726", "%s(%q, …) gives %d path %s: %s", l.Fn, l.Name, len(l.Args), countWord(len(l.Args), "argument", "arguments"), nr.signature())
		return append(out, linkProblem{d.WithHelp("give the path's parameters in its order%s", queryHelp(nr)), -1})
	}
	for i, a := range l.Args {
		p := nr.Params[i]
		if !typed || argFits(a, p) {
			continue
		}
		d := diag.New("E0726", "%s(%q, …): its %s argument is %s, and %s", l.Fn, l.Name, ordinalWord(i+1), articled(shortType(a.Type)), nr.signature())
		out = append(out, linkProblem{d.WithHelp("convert it, or give the path parameter %s another value", p.Name), -1})
	}
	for ki, k := range l.Keys {
		if slices.ContainsFunc(nr.Query, func(q routeQuery) bool { return q.Key == k }) {
			continue
		}
		var keys []string
		for _, q := range nr.Query {
			keys = append(keys, q.Key)
		}
		d := diag.New("E0727", "%s(%q, query{%q: …}): %s keeps no ?%s in its query", l.Fn, l.Name, k, nr.Page, k)
		switch s := closest(k, keys); {
		case s != "":
			d = d.WithHelp("the page's @ui.URL fields keep %s", strings.Join(keys, ", ")).WithSuggest(s)
		case len(keys) > 0:
			d = d.WithHelp("the page's @ui.URL fields keep %s", strings.Join(keys, ", "))
		default:
			d = d.WithHelp("the page keeps nothing in its query: mark a field of it @ui.URL(%q) to read ?%s", k, k)
		}
		out = append(out, linkProblem{d, ki})
	}
	return out
}

func ordinalWord(n int) string {
	if w := []string{"first", "second", "third", "fourth", "fifth"}; n <= len(w) {
		return w[n-1]
	}
	return fmt.Sprintf("%dth", n)
}

func queryHelp(nr *namedRoute) string {
	if len(nr.Query) == 0 {
		return ""
	}
	return ", then query{…} for its query"
}

func articled(s string) string {
	if s == "" {
		return "a value"
	}
	if strings.ContainsRune("aeiouAEIOU", rune(s[0])) {
		return "an " + s
	}
	return "a " + s
}

// argFits reports whether an argument of a can be written as the
// parameter p.
func argFits(a argFact, p routeParam) bool {
	switch {
	case p.Type == "":
		return a.KindOK
	case a.Type == p.Type:
		return true
	case !a.KindOK:
		return false
	}
	switch p.Kind {
	case paramInt, paramUint:
		return a.Kind == paramInt || a.Kind == paramUint
	case paramString, paramText:
		return a.Kind == paramString
	case paramRest:
		return a.Kind == paramString || a.Kind == paramRest
	}
	return false
}

// linkSpan is where a link's diagnostic goes: its name, or its query's
// key key; the line of its generated Go when its source has no such call.
func (c *routeChecker) linkSpan(l linkFact, key int) diag.Span {
	at := diag.Span{File: l.File, Line: l.Line}
	src, err := load.ReadSource(l.File)
	if err != nil {
		return at
	}
	n := 0
	for _, rc := range findRouteCalls(src) {
		if rc.name != l.Name || rc.fn != l.Fn {
			continue
		}
		if n++; n-1 != l.Nth {
			continue
		}
		from, to := rc.nameStart, rc.nameEnd
		if key >= 0 && rc.query != nil && key < len(rc.query.keys) {
			from, to = rc.query.keys[key].start, rc.query.keys[key].end
		}
		line, col := lineCol(src, from)
		_, endCol := lineCol(src, to)
		return diag.Span{File: l.File, Line: line, Col: col, EndCol: endCol}
	}
	return at
}
