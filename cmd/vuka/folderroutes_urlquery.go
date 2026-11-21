package main

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
)

// A page whose component keeps fields in the query with @ui.URL("name")
// gets a struct in urls, a field for each, and its URL takes one:
// urls.Orgs.Org(7).Edit.URL(ctx, urls.OrgsOrgEdit{Tab: "x"}).

// urlQuery is the query of one page.
type urlQuery struct {
	dir    *routeDir
	typ    string // the page's type
	segs   []string
	where  string // the page's place under pages/
	name   string // the struct's name
	fields []queryField
}

// queryField is a field of the page's component kept in the query.
type queryField struct {
	field string // in the component
	name  string // in the struct: field, exported
	key   string
	typ   types.Type     // nil in the stub the pages are transpiled with
	def   constant.Value // the @ui.URL's default, when it is a constant
}

// active reports whether urls has a struct for the query.
func (q *urlQuery) active() bool { return q != nil && len(q.fields) > 0 }

// addQuery records the query of the page typ of d at the folder n: the
// field leaf, or the folder itself when leaf is "". Its fields are those
// of the page's Vuka source marked @ui.URL("name"), not the bare ones.
func (n *urlNode) addQuery(leaf string, d *routeDir, typ string) {
	if typ == "" {
		return
	}
	q := &urlQuery{dir: d, typ: typ}
	for _, a := range structURLAttrs(d.dir, typ) {
		name := upperFirst(a.field)
		if a.bare || a.key == "" || slices.ContainsFunc(q.fields, func(f queryField) bool { return f.name == name }) {
			continue
		}
		q.fields = append(q.fields, queryField{field: a.field, name: name, key: a.key})
	}
	if len(q.fields) == 0 {
		return
	}
	q.segs = n.nameSegs(leaf)
	q.where = "pages/" + joinRel(n.rel, kebab(leaf))
	if n.queries == nil {
		n.queries = map[string]*urlQuery{}
	}
	if n.queries[leaf] == nil {
		n.queries[leaf] = q
	}
}

// nameQueries names the structs: a page's path in PascalCase, AjiraAdvertsView
// for pages/ajira/adverts/view, less the leading folders every page is in
// (AdvertsView) when the names stay unique; one equal to a name of the root
// value gets Query, and any other repeat a number.
func (root *urlNode) nameQueries() {
	var all []*urlQuery
	var pages [][]string
	var walk func(n *urlNode)
	walk = func(n *urlNode) {
		if n.index && n.rel != "" {
			pages = append(pages, n.nameSegs(""))
		}
		for _, p := range n.pages {
			pages = append(pages, n.nameSegs(p))
		}
		for _, leaf := range slices.Sorted(maps.Keys(n.queries)) {
			all = append(all, n.queries[leaf])
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(root)
	if len(all) == 0 {
		return
	}
	shared := 0
	if len(pages) > 0 {
		shared = len(pages[0]) - 1
		for _, p := range pages {
			shared = min(shared, len(p)-1)
			for i := 0; i < shared; i++ {
				if p[i] != pages[0][i] {
					shared = i
				}
			}
		}
	}
	reserved := map[string]bool{"Root": true}
	for _, v := range rootVars(root) {
		reserved[v] = true
	}
	name := func(q *urlQuery, drop int) string {
		if len(q.segs) <= drop {
			return "Index"
		}
		return strings.Join(q.segs[drop:], "")
	}
	for _, drop := range []int{shared, 0} {
		seen := map[string]bool{}
		ok := true
		for _, q := range all {
			nm := name(q, drop)
			ok = ok && !seen[nm] && !reserved[nm]
			seen[nm] = true
		}
		if ok {
			for _, q := range all {
				q.name = name(q, drop)
			}
			return
		}
	}
	taken := maps.Clone(reserved)
	slices.SortFunc(all, func(a, b *urlQuery) int { return strings.Compare(strings.Join(a.segs, "/"), strings.Join(b.segs, "/")) })
	for _, q := range all {
		base := name(q, 0)
		if reserved[base] {
			base += "Query"
		}
		q.name = base
		for i := 2; taken[q.name]; i++ {
			q.name = fmt.Sprintf("%s%d", base, i)
		}
		taken[q.name] = true
	}
}

// nameSegs is the names of the folders down to the page leaf of n, and
// leaf: the parts of its struct's name.
func (n *urlNode) nameSegs(leaf string) []string {
	var segs []string
	for _, seg := range strings.Split(n.rel, "/") {
		if seg != "" {
			segs = append(segs, (&urlNode{seg: seg}).name())
		}
	}
	if leaf != "" {
		segs = append(segs, leaf)
	}
	return segs
}

// resolveQueries types the queries of the tree from the checked page
// packages: a field whose type a URL can't hold, or urls can't name, is
// left out with W0705, and the query goes with its last field.
func (c *routeChecker) resolveQueries(n *urlNode) {
	for _, q := range n.queries {
		q.fields = c.queryTypes(q)
	}
	for _, ch := range n.children {
		c.resolveQueries(ch)
	}
}

func (c *routeChecker) queryTypes(q *urlQuery) []queryField {
	in := c.info[q.dir]
	if in == nil || in.pkg == nil {
		return nil
	}
	tn, _ := in.pkg.Scope().Lookup(q.typ).(*types.TypeName)
	if tn == nil {
		return nil
	}
	st, _ := tn.Type().Underlying().(*types.Struct)
	if st == nil {
		return nil
	}
	syn := c.r.syntax[q.dir.importPath]
	defaults := queryDefaults(syn, q.typ)
	var out []queryField
	for _, f := range q.fields {
		var v *types.Var
		for i := range st.NumFields() {
			if st.Field(i).Name() == f.field {
				v = st.Field(i)
			}
		}
		if v == nil {
			continue
		}
		t := v.Type()
		if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Name() == "Assign" && n.TypeArgs().Len() == 1 {
			t = n.TypeArgs().At(0)
		}
		if why := c.queryProblem(t); why != "" {
			c.report(diag.Warn("W0705", "%s.%s is kept in ?%s, and urls leaves it out of the page's query: %s", q.typ, f.field, f.key, why), v.Pos()).
				WithHelp("declare the type outside pages/, or a type with a text form: the field reads ?%s as always, and the URL can carry it by hand", f.key)
			continue
		}
		f.typ = t
		if e, ok := defaults[f.field]; ok {
			f.def = constOf(e, syn.uses)
		}
		out = append(out, f)
	}
	return out
}

// queryDefaults are the arguments of the .Default of the @ui.URL attributes
// in typ's LiveSchema, by field.
func queryDefaults(syn checked, typ string) map[string]ast.Expr {
	out := map[string]ast.Expr{}
	for _, f := range syn.files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Recv == nil || len(fd.Recv.List) == 0 || recvTypeName(fd.Recv.List[0].Type) != typ || fd.Name.Name != "LiveSchema" {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) < 2 {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); !ok || sel.Sel.Name != "Field" {
					return true
				}
				addr, ok := call.Args[0].(*ast.UnaryExpr)
				if !ok {
					return true
				}
				fs, ok := addr.X.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				for _, opt := range call.Args[1:] {
					for e := opt; ; {
						c, ok := e.(*ast.CallExpr)
						if !ok {
							break
						}
						sel, ok := c.Fun.(*ast.SelectorExpr)
						if !ok {
							break
						}
						if sel.Sel.Name == "Default" && len(c.Args) == 1 {
							out[fs.Sel.Name] = c.Args[0]
						}
						e = sel.X
					}
				}
				return true
			})
		}
	}
	return out
}

// constOf is the constant e is: a literal, a signed one, or a constant's
// name.
func constOf(e ast.Expr, uses map[*ast.Ident]types.Object) constant.Value {
	switch e := e.(type) {
	case *ast.BasicLit:
		if v := constant.MakeFromLiteral(e.Value, e.Kind, 0); v.Kind() != constant.Unknown {
			return v
		}
	case *ast.ParenExpr:
		return constOf(e.X, uses)
	case *ast.UnaryExpr:
		if e.Op == token.SUB || e.Op == token.ADD {
			if v := constOf(e.X, uses); v != nil {
				return constant.UnaryOp(e.Op, v, 0)
			}
		}
	case *ast.Ident:
		if k, ok := uses[e].(*types.Const); ok {
			return k.Val()
		}
	case *ast.SelectorExpr:
		if k, ok := uses[e.Sel].(*types.Const); ok {
			return k.Val()
		}
	}
	return nil
}

// queryProblem is why urls can't hold a field of type t in a page's query,
// "" when it can: the type must be one @ui.URL keeps, and one urls can
// import.
func (c *routeChecker) queryProblem(t types.Type) string {
	if !urlText(t) {
		return t.String() + " has no text form a URL can hold: a string, a number, a bool, a type with UnmarshalText, or a slice or pointer of one"
	}
	return c.unnameable(t, map[types.Type]bool{})
}

// urlText reports whether a URL parameter's text can set a t, as ui's
// @ui.URL checks it.
func urlText(t types.Type) bool {
	if ms := types.NewMethodSet(types.NewPointer(t)).Lookup(nil, "UnmarshalText"); ms != nil {
		if sig, ok := ms.Type().(*types.Signature); ok && sig.Params().Len() == 1 && sig.Results().Len() == 1 {
			return true
		}
	}
	switch u := t.Underlying().(type) {
	case *types.Pointer:
		switch u.Elem().Underlying().(type) {
		case *types.Pointer, *types.Slice:
			return false
		}
		return urlText(u.Elem())
	case *types.Slice:
		switch u.Elem().Underlying().(type) {
		case *types.Pointer, *types.Slice:
			return false
		}
		return urlText(u.Elem())
	case *types.Basic:
		return u.Info()&(types.IsString|types.IsBoolean|types.IsInteger|types.IsFloat) != 0 && u.Info()&types.IsUntyped == 0 && u.Kind() != types.Uintptr
	}
	return false
}

// unnameable is why the package urls can't write t, "" when it can: a
// type it can't reach, an unexported one, or one of a package that imports
// urls.
func (c *routeChecker) unnameable(t types.Type, seen map[types.Type]bool) string {
	if seen[t] {
		return ""
	}
	seen[t] = true
	switch t := types.Unalias(t).(type) {
	case *types.Named:
		obj := t.Obj()
		if obj.Pkg() != nil {
			switch {
			case !obj.Exported():
				return obj.Name() + " is unexported"
			case c.r.byPath[obj.Pkg().Path()] != nil:
				return obj.Pkg().Path() + " is a page package, which urls can't import"
			case importsPath(obj.Pkg(), c.t.pagesPath+"/"+urlsDirName, map[*types.Package]bool{}):
				return obj.Pkg().Path() + " imports urls, and urls can't import it back"
			}
		}
		for i := range t.TypeArgs().Len() {
			if why := c.unnameable(t.TypeArgs().At(i), seen); why != "" {
				return why
			}
		}
	case *types.Pointer:
		return c.unnameable(t.Elem(), seen)
	case *types.Slice:
		return c.unnameable(t.Elem(), seen)
	}
	return ""
}

// importsPath reports whether p imports the package path, directly or not.
func importsPath(p *types.Package, path string, seen map[*types.Package]bool) bool {
	if seen[p] {
		return false
	}
	seen[p] = true
	for _, im := range p.Imports() {
		if im.Path() == path || importsPath(im, path, seen) {
			return true
		}
	}
	return false
}

// decl writes the struct of q; typeOf writes a field's type.
func (q *urlQuery) decl(b *strings.Builder, typeOf func(queryField) string) {
	fmt.Fprintf(b, "\n// %s is the query of %s.\n//\n// Each field is a @ui.URL field of the page, written as a parameter unless it\n// is at its zero value or default. The name is the page's path in PascalCase, less the\n// leading folders every page is in when that stays unique.\ntype %s struct {\n", q.name, q.where, q.name)
	for _, f := range q.fields {
		fmt.Fprintf(b, "\t%s %s // ?%s=\n", f.name, typeOf(f), f.key)
	}
	b.WriteString("}\n")
}

// valuesMethod writes q's values: the parameters in key order.
func (q *urlQuery) valuesMethod(b *strings.Builder, urlQ string, typeOf func(types.Type) string) {
	fmt.Fprintf(b, "\nfunc (q %s) values() %s.Values {\n\tv := %[2]s.Values{}\n", q.name, urlQ)
	fields := slices.Clone(q.fields)
	slices.SortStableFunc(fields, func(a, b queryField) int { return strings.Compare(a.key, b.key) })
	for _, f := range fields {
		fmt.Fprintf(b, "\tsetParam(v, %q, q.%s%s)\n", f.key, f.name, q.defaultArg(f, typeOf))
	}
	b.WriteString("\treturn v\n}\n")
}

// defaultArg is the argument giving setParam the field's default, written as
// a value of the field's type.
func (q *urlQuery) defaultArg(f queryField, typeOf func(types.Type) string) string {
	if f.def == nil {
		return ""
	}
	var lit string
	switch f.def.Kind() {
	case constant.String:
		lit = strconv.Quote(constant.StringVal(f.def))
	case constant.Bool:
		lit = strconv.FormatBool(constant.BoolVal(f.def))
	case constant.Int:
		lit = f.def.ExactString()
	case constant.Float:
		x, _ := constant.Float64Val(f.def)
		lit = strconv.FormatFloat(x, 'g', -1, 64)
	default:
		return ""
	}
	b, ok := f.typ.Underlying().(*types.Basic)
	if !ok {
		return ""
	}
	if _, named := types.Unalias(f.typ).(*types.Named); !named && (b.Kind() == types.String && f.def.Kind() == constant.String ||
		b.Kind() == types.Bool && f.def.Kind() == constant.Bool || b.Kind() == types.Int && f.def.Kind() == constant.Int) {
		return ", " + lit
	}
	return ", " + typeOf(f.typ) + "(" + lit + ")"
}

// queryHelpers are what the structs share, written for the names the file
// gives the packages it imports.
func queryHelpers(ctx, web, url, reflect, slices, ui string) string {
	return strings.NewReplacer("«ctx»", ctx, "«web»", web, "«url»", url, "«reflect»", reflect, "«slices»", slices, "«ui»", ui).Replace(`
// queryValues is the query of a page.
type queryValues interface{ values() «url».Values }

// queryPage is a page's path, and its URL with the query Q.
type queryPage[Q queryValues] struct{ path string }

// Path is the page's path.
func (p queryPage[Q]) Path() string { return p.path }

// URL is Path carrying the app's mount prefix, and the query q: only its
// first value is used.
func (p queryPage[Q]) URL(ctx «ctx».Context, q ...Q) string {
	return withQuery(«web».MountPath(ctx, p.path), q)
}

// withQuery is path with the query q, if any.
func withQuery[Q queryValues](path string, q []Q) string {
	if len(q) > 0 {
		if s := q[0].values().Encode(); s != "" {
			return path + "?" + s
		}
	}
	return path
}

// setParam adds the parameter key to v as a @ui.URL field writes it, unless
// value is its zero value or def.
func setParam(v «url».Values, key string, value any, def ...any) {
	text := «ui».QueryText(value)
	if len(text) == 0 || «reflect».ValueOf(value).IsZero() || len(def) > 0 && «slices».Equal(text, «ui».QueryText(def[0])) {
		return
	}
	v[key] = text
}
`)
}
