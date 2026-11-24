package main

import (
	"slices"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
)

// Route names: every path of the table is named by its folders, with dots
// (users.edit for /users/{user}/edit); parameter and group folders add
// nothing, and a file adds its segment (orders.export.download). Two paths
// alike but for their parameters (/orders, /orders/{order}) each keep the
// parameters they have: orders, orders.order. route("users.edit", id)
// links by name.

// namedRoute is a route name and what a link to it takes.
type namedRoute struct {
	Name   string
	Path   string       // /users/{user}/edit
	Params []routeParam // its path parameters, in order
	Query  []routeQuery // its page's @ui.URL("key") fields
	Page   string       // the page or endpoint, as the table names it
	At     diag.Span    // where it is declared
}

// routeParam is a path parameter a link gives.
type routeParam struct {
	Name string
	Type string    // as Go writes it, packages by path; "" when nothing binds it well
	Kind paramKind // how a value of it is written
}

// routeQuery is a query key a link to a page may give.
type routeQuery struct {
	Key, Field, Type string
}

// routeBaseName is a path's name without its parameters; routeFullName with them.
func routeBaseName(path string) string { return pathName(path, false) }
func routeFullName(path string) string { return pathName(path, true) }

func pathName(path string, params bool) string {
	if path == "/" {
		return "index"
	}
	var parts []string
	for _, seg := range splitPath(path) {
		if strings.HasPrefix(seg, "{") {
			if !params {
				continue
			}
			seg = strings.TrimSuffix(strings.Trim(seg, "{}"), "...")
		}
		parts = append(parts, seg)
	}
	return strings.Join(parts, ".")
}

// nameRoutes names the paths: each its base name, or, when another path
// has the same base name, its full one.
func nameRoutes(paths []string) map[string]string {
	bases := map[string]int{}
	for _, p := range paths {
		bases[routeBaseName(p)]++
	}
	out := make(map[string]string, len(paths))
	for _, p := range paths {
		n := routeBaseName(p)
		if bases[n] > 1 || n == "" {
			n = routeFullName(p)
		}
		out[p] = n
	}
	return out
}

// names names the table's routes and records what a link to each takes;
// two paths named alike are E0706.
func (c *routeChecker) names() {
	var paths []string
	first := map[string]*folderRoute{}
	for _, rt := range c.res.routes {
		if f, ok := first[rt.Path]; !ok {
			paths = append(paths, rt.Path)
			first[rt.Path] = rt
		} else if f.typ == "" || f.Kind == "endpoint" && (rt.Kind == "page" || rt.Kind == "live") {
			first[rt.Path] = rt
		}
	}
	slices.Sort(paths)
	names := nameRoutes(paths)
	c.t.named = map[string]*namedRoute{}
	for _, p := range paths {
		rt, name := first[p], names[p]
		at := diag.Span{File: rt.File, Line: rt.Line}
		if prev := c.t.named[name]; prev != nil {
			c.reportAt(diag.New("E0706", "the route %s is named %s, and so is %s", p, name, prev.Path), at).
				WithRelated(diag.Span{File: prev.At.File, Line: prev.At.Line, Label: prev.Path + " is named " + name + " here"}).
				WithHelp("rename one of the folders: a route's name is its folders' with dots, and a dot in a folder's name runs into the next")
			continue
		}
		nr := &namedRoute{Name: name, Path: p, Page: rt.Page, At: at}
		for _, key := range rt.params {
			pp := c.params[key]
			if pp == nil {
				continue
			}
			rp := routeParam{Name: pp.name, Kind: pp.p.kind}
			if pp.rest {
				rp.Kind = paramRest
			}
			if pp.p.typ != nil {
				rp.Type = pp.p.typ.render(func(path, _ string) string { return path })
			}
			nr.Params = append(nr.Params, rp)
		}
		if rt.typ != "" && (rt.Kind == "page" || rt.Kind == "live") && rt.dir != nil {
			var facts []queryFact
			if in := c.info[rt.dir]; in != nil && in.facts != nil {
				facts = in.facts.Queries[rt.typ]
			}
			for _, a := range rt.dir.attrs.structURLAttrs(rt.dir.dir, rt.typ) {
				if a.bare || a.key == "" || slices.ContainsFunc(nr.Query, func(q routeQuery) bool { return q.Key == a.key }) {
					continue
				}
				q := routeQuery{Key: a.key, Field: a.field}
				if i := slices.IndexFunc(facts, func(f queryFact) bool { return f.Field == a.field }); i >= 0 {
					q.Type = facts[i].TypeStr
				}
				nr.Query = append(nr.Query, q)
			}
			slices.SortFunc(nr.Query, func(a, b routeQuery) int { return strings.Compare(a.Key, b.Key) })
		}
		c.t.named[name] = nr
		for _, r := range c.res.routes {
			if r.Path == p {
				r.name = name
			}
		}
	}
}

// signature is how a message names the route and what it takes:
// users.edit (/users/[user]/edit) takes user int64.
func (r *namedRoute) signature() string {
	s := r.Name + " (" + bracketPath(r.Path) + ")"
	if len(r.Params) == 0 {
		return s + " takes no path parameters"
	}
	var ps []string
	for _, p := range r.Params {
		ps = append(ps, strings.TrimSpace(p.Name+" "+shortType(p.Type)))
	}
	return s + " takes " + strings.Join(ps, ", ")
}

// bracketPath is a table path as the folders spell it: /users/[user]/edit.
func bracketPath(path string) string {
	segs := splitPath(path)
	for i, s := range segs {
		if strings.HasPrefix(s, "{") {
			segs[i] = "[" + strings.Trim(s, "{}") + "]"
		}
	}
	return "/" + strings.Join(segs, "/")
}

// shortType is a type as a message names it: its packages by name.
func shortType(t string) string {
	var b strings.Builder
	for len(t) > 0 {
		i := strings.IndexAny(t, "*[]() ,")
		word := t
		if i >= 0 {
			word = t[:i]
		}
		if j := strings.LastIndexByte(word, '/'); j >= 0 {
			word = word[j+1:]
		}
		b.WriteString(word)
		if i < 0 {
			break
		}
		b.WriteByte(t[i])
		t = t[i+1:]
	}
	return b.String()
}
