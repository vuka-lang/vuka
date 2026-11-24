package main

import (
	"fmt"
	"go/token"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// The package urls (pages/urls/urls_gen.go) is the paths of the pages as a
// chain of values: urls.Orgs.Org(7).Edit.Path(). A folder is a value with a
// field per static child folder and per page (the Index aside), a method per
// parameter child folder, and Path when it has an Index. It imports no page
// package, so any page links to any other.

// urlNode is a folder of the chain.
type urlNode struct {
	seg      string // the folder's name; "" for pages/
	rel      string
	param    bool
	p        urlParam // a parameter folder's parameter (not in the stub)
	index    bool
	pages    []string // its pages but Index
	children []*urlNode
	typ      string
	dir      *routeDir
	queries  map[string]*urlQuery // by page; "" for the folder's own
}

func (n *urlNode) child(seg string) *urlNode {
	for _, c := range n.children {
		if c.seg == seg {
			return c
		}
	}
	rel := seg
	if n.rel != "" {
		rel = n.rel + "/" + seg
	}
	c := &urlNode{seg: seg, rel: rel}
	n.children = append(n.children, c)
	slices.SortFunc(n.children, func(a, b *urlNode) int { return strings.Compare(a.seg, b.seg) })
	return c
}

// find is the folder at rel, nil when there is none.
func (n *urlNode) find(rel string) *urlNode {
	for _, seg := range strings.Split(rel, "/") {
		i := slices.IndexFunc(n.children, func(c *urlNode) bool { return c.seg == seg })
		if i < 0 {
			return nil
		}
		n = n.children[i]
	}
	return n
}

func (n *urlNode) at(rel string) *urlNode {
	if rel == "" {
		return n
	}
	for _, seg := range strings.Split(rel, "/") {
		n = n.child(seg)
	}
	return n
}

// prune drops the folders with no page below them, and reports whether n
// has one.
func (n *urlNode) prune() bool {
	n.children = slices.DeleteFunc(n.children, func(c *urlNode) bool { return !c.prune() })
	return n.index || len(n.pages) > 0 || len(n.children) > 0
}

// name is how a folder is named in its parent's value: JobPermits for
// job-permits, ID for a parameter [id].
func (n *urlNode) name() string {
	if load.Bracketed(n.seg) {
		return goIdent(bracketName(n.seg))
	}
	var b strings.Builder
	for _, part := range strings.FieldsFunc(n.seg, func(r rune) bool { return r == '-' || r == '_' || r == '.' || r == ' ' }) {
		b.WriteString(upperFirst(part))
	}
	return b.String()
}

// nameTypes gives each folder its type: rootURL, orgsOrgURL.
func (n *urlNode) nameTypes(taken map[string]bool) {
	base := "root"
	if n.rel != "" {
		var b strings.Builder
		for _, seg := range strings.Split(n.rel, "/") {
			b.WriteString((&urlNode{seg: seg}).name())
		}
		base = strings.ToLower(b.String()[:1]) + b.String()[1:]
	}
	n.typ = base + "URL"
	for i := 2; taken[n.typ]; i++ {
		n.typ = fmt.Sprintf("%sURL%d", base, i)
	}
	taken[n.typ] = true
	for _, c := range n.children {
		c.nameTypes(taken)
	}
}

// urlNodes is the chain of folders and pages as the files under pages/
// name them, whether or not their packages type-check: what a page's path is
// doesn't depend on its component. A type with no markup is a page when
// isPage says so (nil: it is); one in a .vuka.html file always is. The
// queries are the fields its source marks @ui.URL, to be typed by the check.
func (t *routeTree) urlNodes(isPage func(d *routeDir, typ string) bool) *urlNode {
	root := &urlNode{}
	for _, d := range t.dirs {
		rel := urlRel(d.rel)
		n := root.at(rel)
		for _, p := range d.pages {
			if isPage != nil && d.fileOf[p] == "" && !isPage(d, p) {
				continue
			}
			switch name := d.urlName(p); {
			case load.Bracketed(name):
				at := root.at(joinRel(rel, name))
				at.index, at.dir = true, d
				at.addQuery("", d, p)
			case name == "Index":
				n.index, n.dir = true, d
				n.addQuery("", d, p)
			case !slices.Contains(n.pages, name):
				n.pages = append(n.pages, name)
				n.dir = d
				n.addQuery(name, d, p)
			}
		}
		for _, f := range d.funcs {
			root.at(joinRel(rel, fileSeg(f.file))).index = true
		}
	}
	root.mark()
	root.prune()
	root.nameTypes(map[string]bool{"pageURL": true})
	root.nameQueries()
	return root
}

// urlsStub is the package urls the pages are transpiled with, read from
// their sources: a field for every type with a constructor, parameters
// taken as ...any.
func (t *routeTree) urlsStub() []byte {
	root := t.urlNodes(nil)
	var b strings.Builder
	b.WriteString(routesHeading + "package " + urlsDirName + "\n\nimport (\n\t\"context\"\n\n\tweb \"github.com/vuka-lang/web/v2\"\n)\n\nvar Root = new" + upperFirst(root.typ) + "(\"\")\n")
	b.WriteString("\ntype pageURL struct{ path string }\n\nfunc (p pageURL) Path() string { return p.path }\n\nfunc (p pageURL) URL(ctx context.Context) string { return web.MountPath(ctx, p.path) }\n")
	var queries []*urlQuery
	root.eachQuery(func(q *urlQuery) { queries = append(queries, q) })
	if len(queries) > 0 {
		b.WriteString("\ntype queryPage[Q any] struct{ path string }\n\nfunc (p queryPage[Q]) Path() string { return p.path }\n\nfunc (p queryPage[Q]) URL(ctx context.Context, q ...Q) string { return web.MountPath(ctx, p.path) }\n")
		slices.SortFunc(queries, func(a, b *urlQuery) int { return strings.Compare(a.name, b.name) })
		for _, q := range queries {
			b.WriteString("\ntype " + q.name + " struct{\n")
			for _, f := range q.fields {
				b.WriteString("\t" + f.name + " any\n")
			}
			b.WriteString("}\n")
		}
	}
	var walk func(n *urlNode)
	walk = func(n *urlNode) {
		taken := map[string]bool{"Path": n.index}
		fmt.Fprintf(&b, "\ntype %s struct {\n\tpath string\n", n.typ)
		var inits []string
		for _, c := range n.children {
			if name := c.name(); !c.param && !taken[name] {
				taken[name] = true
				fmt.Fprintf(&b, "\t%s %s\n", name, c.typ)
				inits = append(inits, fmt.Sprintf("%s: new%s(\"\")", name, upperFirst(c.typ)))
			}
		}
		for _, p := range n.pages {
			if !taken[p] {
				taken[p] = true
				if q := n.queries[p]; q.active() {
					fmt.Fprintf(&b, "\t%s queryPage[%s]\n", p, q.name)
				} else {
					fmt.Fprintf(&b, "\t%s pageURL\n", p)
				}
			}
		}
		b.WriteString("}\n")
		fmt.Fprintf(&b, "\nfunc new%s(path string) %s { return %s{%s} }\n", upperFirst(n.typ), n.typ, n.typ, strings.Join(inits, ", "))
		if n.index {
			fmt.Fprintf(&b, "\nfunc (u %s) Path() string { return u.path }\n", n.typ)
			if q := n.queries[""]; q.active() {
				fmt.Fprintf(&b, "\nfunc (u %s) URL(ctx context.Context, q ...%s) string { return web.MountPath(ctx, u.path) }\n", n.typ, q.name)
			} else {
				fmt.Fprintf(&b, "\nfunc (u %s) URL(ctx context.Context) string { return web.MountPath(ctx, u.path) }\n", n.typ)
			}
		}
		for _, c := range n.children {
			if name := c.name(); c.param && !taken[name] {
				taken[name] = true
				fmt.Fprintf(&b, "\nfunc (u %s) %s(...any) %s { return new%s(\"\") }\n", n.typ, name, c.typ, upperFirst(c.typ))
			}
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(root)
	if vars := rootVars(root); len(vars) > 0 {
		b.WriteString("\nvar (\n")
		for _, v := range vars {
			fmt.Fprintf(&b, "\t%s = Root.%s\n", v, v)
		}
		b.WriteString(")\n")
	}
	return gofmt(b.String())
}

// rootVars are the names of the root folder's children and pages, each
// also a variable of the package.
func rootVars(root *urlNode) []string {
	var out []string
	for _, c := range root.children {
		out = append(out, c.name())
	}
	out = append(out, root.pages...)
	slices.Sort(out)
	return slices.Compact(out)
}

// urlTree builds the chain from the route table, reporting the names that
// collide in a folder's value (E0706) and the parameters urls can't name
// (E0705).
func (c *routeChecker) urlTree() *urlNode {
	routed := map[*routeDir]map[string]bool{}
	for _, r := range c.res.routes {
		if r.dir != nil && r.Kind != "handler" {
			if routed[r.dir] == nil {
				routed[r.dir] = map[string]bool{}
			}
			routed[r.dir][r.typ] = true
		}
	}
	root := c.t.urlNodes(func(d *routeDir, typ string) bool { return routed[d][typ] })
	for key, p := range c.params {
		if n := root.find(key); n != nil {
			n.p = p.p
		}
	}
	c.resolveQueries(root)
	pageAt := func(n *urlNode, page string) diag.Span {
		for _, r := range c.res.routes {
			if r.node == n.rel && r.leaf == page && r.Kind != "handler" {
				return diag.Span{File: r.File, Line: r.Line}
			}
		}
		return diag.Span{}
	}
	var walk func(n *urlNode)
	walk = func(n *urlNode) {
		where := map[string]string{}
		if n.index {
			where["Path"] = "its Path method"
		}
		if n.rel == "" {
			where["Root"] = "the variable urls.Root"
		}
		folder := "pages/" + n.rel
		for _, ch := range n.children {
			name := ch.name()
			what := "the folder " + ch.seg
			if prev, ok := where[name]; ok || !token.IsIdentifier(name) || !token.IsExported(name) {
				at := diag.Span{File: filepath.Join(c.t.pages, filepath.FromSlash(ch.rel))}
				if ch.dir != nil {
					at.File = ch.dir.dir
				}
				if !ok {
					prev = "a Go identifier"
				}
				c.res.diags = append(c.res.diags, *diag.New("E0706", "%s: in urls, %s is named %s, and so is %s", folder, what, name, prev).At(at).
					WithHelp("rename the folder or the page: each is a field or method of the folder's value in urls"))
				continue
			}
			where[name] = what
		}
		for _, p := range n.pages {
			if prev, ok := where[p]; ok {
				c.res.diags = append(c.res.diags, *diag.New("E0706", "%s: in urls, the page %s is named %s, and so is %s", folder, p, p, prev).At(pageAt(n, p)).
					WithHelp("rename the folder or the page: each is a field or method of the folder's value in urls"))
				continue
			}
			where[p] = "the page " + p
		}
		for _, ch := range n.children {
			walk(ch)
		}
	}
	walk(root)
	return root
}

// urlsFile is pages/urls/urls_gen.go.
func (t *routeTree) urlsFile() []byte {
	root := t.urls
	var names []string
	var collect func(n *urlNode)
	collect = func(n *urlNode) {
		if n.param {
			names = append(names, paramIdent(bracketName(n.seg)))
		}
		for _, c := range n.children {
			collect(c)
		}
	}
	collect(root)
	im := newImports(t.pagesPath+"/"+urlsDirName, nil, append(names, "u", "p", "path", "text", "segs", "seg", "i")...)
	needURL := false
	var scanURL func(n *urlNode)
	scanURL = func(n *urlNode) {
		if n.index || len(n.pages) > 0 {
			needURL = true
		}
		for _, c := range n.children {
			scanURL(c)
		}
	}
	scanURL(root)
	var ctxQ, webQ string
	if needURL {
		ctxQ = im.name("context", "context")
		webQ = im.name("github.com/vuka-lang/web/v2", "web")
	}
	var queries []*urlQuery
	root.eachQuery(func(q *urlQuery) { queries = append(queries, q) })
	slices.SortFunc(queries, func(a, b *urlQuery) int { return strings.Compare(a.name, b.name) })
	var urlQ, reflectQ, slicesQ, uiQ string
	if len(queries) > 0 {
		urlQ, reflectQ, slicesQ, uiQ = im.name("net/url", "url"), im.name("reflect", "reflect"), im.name("slices", "slices"), im.name(load.UIPath, "ui")
	}
	var qb strings.Builder
	if len(queries) > 0 {
		qb.WriteString(queryHelpers(ctxQ, webQ, urlQ, reflectQ, slicesQ, uiQ))
		typeOf := func(t goType) string { return t.render(im.name) }
		for _, q := range queries {
			q.decl(&qb, func(f queryField) string { return typeOf(*f.typ) })
			q.valuesMethod(&qb, urlQ, typeOf)
		}
	}
	var b strings.Builder
	pageType := false
	var walk func(n *urlNode)
	walk = func(n *urlNode) {
		where := "pages/"
		if n.rel != "" {
			where += n.rel + "/"
		}
		what := "the folder " + where
		if n.param {
			what = "the parameter " + strings.TrimSuffix(where, "/")
		}
		fmt.Fprintf(&b, "\n// %s is %s.\ntype %s struct {\n\tpath string\n", n.typ, what, n.typ)
		var inits []string
		for _, c := range n.children {
			if !c.param {
				fmt.Fprintf(&b, "\t%s %s\n", c.name(), c.typ)
				inits = append(inits, fmt.Sprintf("%s: new%s(path + %q)", c.name(), upperFirst(c.typ), "/"+c.seg))
			}
		}
		for _, p := range n.pages {
			if q := n.queries[p]; q.active() {
				fmt.Fprintf(&b, "\t%s queryPage[%s]\n", p, q.name)
				inits = append(inits, fmt.Sprintf("%s: queryPage[%s]{path + %q}", p, q.name, "/"+kebab(p)))
				continue
			}
			pageType = true
			fmt.Fprintf(&b, "\t%s pageURL\n", p)
			inits = append(inits, fmt.Sprintf("%s: pageURL{path + %q}", p, "/"+kebab(p)))
		}
		b.WriteString("}\n")
		if len(inits) == 0 {
			fmt.Fprintf(&b, "\nfunc new%s(path string) %s { return %s{path: path} }\n", upperFirst(n.typ), n.typ, n.typ)
		} else {
			fmt.Fprintf(&b, "\nfunc new%s(path string) %s {\n\treturn %s{\n\t\tpath: path,\n", upperFirst(n.typ), n.typ, n.typ)
			for _, i := range inits {
				b.WriteString("\t\t" + i + ",\n")
			}
			b.WriteString("\t}\n}\n")
		}
		if n.index {
			ret := "u.path"
			if n.rel == "" {
				ret = `"/"`
			}
			fmt.Fprintf(&b, "\n// Path is the path of %s.\nfunc (u %s) Path() string { return %s }\n", strings.TrimSuffix(what, "/"), n.typ, ret)
			if q := n.queries[""]; q.active() {
				fmt.Fprintf(&b, "\n// URL is Path carrying the app's mount prefix, and the query q: only its\n// first value is used.\nfunc (u %s) URL(ctx %s.Context, q ...%s) string { return withQuery(%s.MountPath(ctx, %s), q) }\n", n.typ, ctxQ, q.name, webQ, ret)
			} else {
				fmt.Fprintf(&b, "\n// URL is Path carrying the app's mount prefix.\nfunc (u %s) URL(ctx %s.Context) string { return %s.MountPath(ctx, %s) }\n", n.typ, ctxQ, webQ, ret)
			}
		}
		for _, c := range n.children {
			if !c.param {
				continue
			}
			v := paramIdent(c.p.name)
			// nothing binds a parameter whose page doesn't compile: it takes any
			typ, seg := "any", ""
			var body strings.Builder
			if c.p.typ != nil {
				typ = c.p.typ.render(im.name)
				seg = t.segment(&body, im, c.p)
			} else {
				seg = im.name("net/url", "url") + ".PathEscape(" + im.name("fmt", "fmt") + ".Sprint(" + v + "))"
			}
			fmt.Fprintf(&b, "\n// %s is %s%s, for its parameter.\nfunc (u %s) %s(%s %s) %s {\n", c.name(), where, c.seg, n.typ, c.name(), v, typ, c.typ)
			b.WriteString(body.String())
			fmt.Fprintf(&b, "\treturn new%s(u.path + \"/\" + %s)\n}\n", upperFirst(c.typ), seg)
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(root)
	var head strings.Builder
	head.WriteString(routesHeading + "// Package urls is the paths of the pages under pages/, one value per\n// folder: urls.Orgs.Org(7).Edit.Path().\npackage " + urlsDirName + "\n\n")
	head.WriteString(im.block())
	head.WriteString("// Root is the folder pages/.\nvar Root = new" + upperFirst(root.typ) + "(\"\")\n")
	if vars := rootVars(root); len(vars) > 0 {
		head.WriteString("\n// The folders and pages of pages/.\nvar (\n")
		for _, v := range vars {
			fmt.Fprintf(&head, "\t%s = Root.%s\n", v, v)
		}
		head.WriteString(")\n")
	}
	if pageType {
		head.WriteString("\n// pageURL is a page's path.\ntype pageURL struct{ path string }\n\n// Path is the page's path.\nfunc (p pageURL) Path() string { return p.path }\n")
		fmt.Fprintf(&head, "\n// URL is Path carrying the app's mount prefix.\nfunc (p pageURL) URL(ctx %s.Context) string { return %s.MountPath(ctx, p.path) }\n", ctxQ, webQ)
	}
	return gofmtKept(head.String() + qb.String() + b.String())
}

// mark makes each folder named for a parameter one.
func (n *urlNode) mark() {
	n.param = load.Bracketed(n.seg)
	for _, c := range n.children {
		c.mark()
	}
}

// eachQuery calls f with each query of the folder and below that has a
// struct.
func (n *urlNode) eachQuery(f func(*urlQuery)) {
	for _, leaf := range slices.Sorted(maps.Keys(n.queries)) {
		if q := n.queries[leaf]; q.active() {
			f(q)
		}
	}
	for _, c := range n.children {
		c.eachQuery(f)
	}
}
