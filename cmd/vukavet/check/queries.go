package check

import (
	"fmt"
	"go/ast"
	"go/types"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Queries checks orm queries combining tables as the orm does when it
// builds them: a joined model's field used in a query that doesn't join it
// (E0504), Values filling a struct by position with the wrong number or
// kinds of columns (E0505), a SQL fragment whose template isn't a
// constant or whose ? and {n} don't match its arguments (E0506); and it
// warns of a Count a later join can repeat (W0501) and of a join read
// without Values (W0502).
var Queries = &analysis.Analyzer{
	Name: "queries",
	Doc:  "check orm queries that join tables, read them by position and write SQL fragments",
	URL:  docsURL,
	Run:  runQueries,
}

// table is the table a typed field reads: a model's, under an alias when
// joined twice.
type table struct {
	model *types.TypeName
	alias string
}

func (t table) String() string {
	if t.alias != "" {
		return t.model.Name() + " " + strconv.Quote(t.alias)
	}
	return t.model.Name()
}

// joined is a table a chain's Join adds: whether it can repeat the rows
// before it (a join on anything but the joined model's ID, a relation
// holding many rows).
type joined struct {
	t       table
	repeats bool
	at      ast.Node
}

type queryPass struct {
	pass *analysis.Pass
	defs map[types.Object]ast.Expr // a variable's single initial value
}

func runQueries(pass *analysis.Pass) (any, error) {
	if lookup(pass.Pkg, ormPath, "QuerySet") == nil {
		return nil, nil
	}
	q := &queryPass{pass: pass, defs: map[types.Object]ast.Expr{}}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				if len(n.Lhs) == len(n.Rhs) {
					for i, l := range n.Lhs {
						if id, ok := l.(*ast.Ident); ok && pass.TypesInfo.Defs[id] != nil {
							q.defs[pass.TypesInfo.Defs[id]] = n.Rhs[i]
						}
					}
				}
			case *ast.ValueSpec:
				if len(n.Names) == len(n.Values) {
					for i, id := range n.Names {
						q.defs[pass.TypesInfo.Defs[id]] = n.Values[i]
					}
				}
			}
			return true
		})
	}
	seen := map[*ast.CallExpr]bool{}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch funcOf(pass.TypesInfo, call, ormPath) {
			case "SQLOf":
				q.fragment(call, call.Args)
			case "SQLFor":
				if len(call.Args) > 0 {
					if lit, ok := ast.Unparen(call.Args[0]).(*ast.CompositeLit); ok {
						for _, e := range lit.Elts {
							if kv, ok := e.(*ast.KeyValueExpr); ok {
								q.fragment(call, append([]ast.Expr{kv.Value}, call.Args[1:]...))
							}
						}
					}
				}
			}
			if !seen[call] && q.chainCall(call) != "" {
				q.chain(call, seen)
			}
			return true
		})
	}
	return nil, nil
}

// chainCall is the method a call calls on a query: a Manager's, a
// QuerySet's or a Values'.
func (q *queryPass) chainCall(call *ast.CallExpr) string {
	fun := ast.Unparen(call.Fun)
	if ix, ok := fun.(*ast.IndexExpr); ok {
		fun = ix.X
	}
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	s, ok := q.pass.TypesInfo.Selections[sel]
	if !ok || s.Kind() != types.MethodVal {
		return ""
	}
	fn := s.Obj().(*types.Func)
	recv := fn.Type().(*types.Signature).Recv()
	if recv == nil || fn.Pkg() == nil || fn.Pkg().Path() != ormPath {
		return ""
	}
	for _, n := range []string{"Manager", "QuerySet", "Values"} {
		if named(recv.Type(), ormPath, n) {
			return sel.Sel.Name
		}
	}
	return ""
}

// calledName is the name a chain's call calls, where its diagnostics go.
func calledName(call *ast.CallExpr) ast.Node {
	fun := ast.Unparen(call.Fun)
	if ix, ok := fun.(*ast.IndexExpr); ok {
		fun = ix.X
	}
	if sel, ok := fun.(*ast.SelectorExpr); ok {
		return sel.Sel
	}
	return call
}

// link is a call of a chain: its method and call.
type link struct {
	name string
	call *ast.CallExpr
}

// chain checks the query outer ends, its calls back to where it starts.
func (q *queryPass) chain(outer *ast.CallExpr, seen map[*ast.CallExpr]bool) {
	info := q.pass.TypesInfo
	var links []link
	var root ast.Expr = outer
	for {
		call, ok := ast.Unparen(root).(*ast.CallExpr)
		if !ok {
			break
		}
		name := q.chainCall(call)
		if name == "" {
			break
		}
		seen[call] = true
		links = append([]link{{name, call}}, links...)
		fun := ast.Unparen(call.Fun)
		if ix, ok := fun.(*ast.IndexExpr); ok {
			fun = ix.X
		}
		root = fun.(*ast.SelectorExpr).X
	}
	if len(links) == 0 {
		return
	}
	model := queriedModel(info.TypeOf(root))
	// A chain from a manager is the whole query; one from a variable may
	// have joined before.
	whole := model != nil && named(info.TypeOf(root), ormPath, "Manager")
	var joins []joined
	for _, l := range links {
		if l.name == "Join" && len(l.call.Args) > 0 {
			if j, ok := q.join(l.call.Args[0], model); ok {
				joins = append(joins, j)
			}
		}
	}
	terminal := links[len(links)-1].name
	values := false
	nJoin := 0
	for _, l := range links {
		var allowed []joined
		switch l.name {
		case "Join":
			allowed = joins[:min(nJoin+1, len(joins))]
			nJoin++
		case "Values":
			values = true
			q.values(l.call, model, joins)
			allowed = joins
		default:
			allowed = joins
		}
		if whole {
			for _, a := range l.call.Args {
				q.fields(a, model, allowed, joins, l.name == "Join")
			}
		}
	}
	if whole && !values && len(joins) > 0 && (terminal == "All" || terminal == "Iter" || terminal == "First") {
		for _, j := range joins {
			if j.repeats {
				report(q.pass, calledName(links[len(links)-1].call), "W0502", "read the columns with Values (an aggregate among them groups the rows), or aggregate through a relation path",
					"the Join of %s can repeat %s's rows, and %s reads %s rows, one per match", j.t, model.Name(), terminal, model.Name())
				break
			}
		}
	}
}

// queriedModel is T of a Manager[T], QuerySet[T] or Values[T, R].
func queriedModel(t types.Type) *types.TypeName {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.TypeArgs().Len() == 0 {
		return nil
	}
	m, ok := types.Unalias(n.TypeArgs().At(0)).(*types.Named)
	if !ok {
		return nil
	}
	return m.Obj()
}

// fieldsModel is the model of generated typed fields' type, <Model>Fields.
func fieldsModel(t types.Type) *types.TypeName {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return nil
	}
	st, ok := n.Underlying().(*types.Struct)
	if !ok || st.NumFields() == 0 || !st.Field(0).Embedded() || !named(st.Field(0).Type(), ormPath, "RelationPath") {
		return nil
	}
	name, ok := strings.CutSuffix(n.Obj().Name(), "Fields")
	if !ok {
		return nil
	}
	m, _ := n.Obj().Pkg().Scope().Lookup(name).(*types.TypeName)
	return m
}

// typedColumn is whether t is one of the orm's typed fields.
func typedColumn(t types.Type) bool {
	for _, n := range []string{"Field", "TextField", "NumberField", "TimeField"} {
		if is(t, ormPath, n) {
			return true
		}
	}
	return false
}

// tableOf is the table a typed field, typed fields or a relation of them
// read, as far as the package shows: XColumns, a variable holding typed
// fields (x := XColumns.As("x")), and fields and relations reached from them.
func (q *queryPass) tableOf(e ast.Expr, depth int) (table, bool) {
	info := q.pass.TypesInfo
	if depth > 8 {
		return table{}, false
	}
	switch x := ast.Unparen(e).(type) {
	case *ast.Ident:
		obj := info.Uses[x]
		if obj == nil {
			return table{}, false
		}
		if m := fieldsModel(obj.Type()); m != nil && obj.Name() == m.Name()+"Columns" && obj.Parent() == obj.Pkg().Scope() {
			return table{model: m}, true
		}
		if d, ok := q.defs[obj]; ok {
			return q.tableOf(d, depth+1)
		}
	case *ast.SelectorExpr:
		if s, ok := info.Selections[x]; ok && s.Kind() == types.FieldVal {
			return q.tableOf(x.X, depth+1)
		}
		if obj, ok := info.Uses[x.Sel]; ok && obj.Parent() == obj.Pkg().Scope() {
			if m := fieldsModel(obj.Type()); m != nil && obj.Name() == m.Name()+"Columns" {
				return table{model: m}, true
			}
		}
	case *ast.CallExpr:
		sel, ok := ast.Unparen(x.Fun).(*ast.SelectorExpr)
		if !ok || fieldsModel(info.TypeOf(sel.X)) == nil {
			return table{}, false
		}
		t, ok := q.tableOf(sel.X, depth+1)
		if !ok {
			return t, false
		}
		if sel.Sel.Name == "As" && len(x.Args) == 1 {
			alias, ok := str(info, x.Args[0])
			return table{model: t.model, alias: alias}, ok
		}
		return t, true
	}
	return table{}, false
}

// join is the table a Join's argument adds: <Model>Columns.On(…), a
// relation of the queried model, or a relation's On(…).
func (q *queryPass) join(arg ast.Expr, model *types.TypeName) (joined, bool) {
	info := q.pass.TypesInfo
	target := arg
	var conds []ast.Expr
	if call, ok := ast.Unparen(arg).(*ast.CallExpr); ok {
		if sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr); ok && sel.Sel.Name == "On" && fieldsModel(info.TypeOf(sel.X)) != nil {
			target, conds = sel.X, call.Args
		}
	}
	m := fieldsModel(info.TypeOf(target))
	if m == nil {
		return joined{}, false
	}
	if rel, ok := relationCall(target); ok {
		// A relation: its model's table, unaliased; repeating when the
		// relation holds many rows.
		many := true
		if owner := fieldsModel(info.TypeOf(rel.X)); owner != nil {
			if st, ok := owner.Type().Underlying().(*types.Struct); ok {
				for i := range st.NumFields() {
					if st.Field(i).Name() == rel.Sel.Name {
						_, many = st.Field(i).Type().Underlying().(*types.Slice)
					}
				}
			}
		}
		return joined{t: table{model: m}, repeats: many, at: arg}, true
	}
	t, ok := q.tableOf(target, 0)
	if !ok {
		return joined{}, false
	}
	return joined{t: t, repeats: !q.byID(conds, t), at: arg}, true
}

// relationCall is e as a call of a relation of typed fields:
// CategoryColumns.Links().
func relationCall(e ast.Expr) (*ast.SelectorExpr, bool) {
	call, ok := ast.Unparen(e).(*ast.CallExpr)
	if !ok || len(call.Args) > 0 {
		return nil, false
	}
	sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
	return sel, ok && sel.Sel.Name != "As"
}

// byID is whether a join's conditions equate the joined model's ID with
// something: one row at most per row before it.
func (q *queryPass) byID(conds []ast.Expr, t table) bool {
	isID := func(e ast.Expr) bool {
		sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "ID" {
			return false
		}
		got, ok := q.tableOf(sel.X, 0)
		return ok && got == t
	}
	for _, c := range conds {
		call, ok := ast.Unparen(c).(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := ast.Unparen(call.Fun).(*ast.SelectorExpr)
		if !ok || (sel.Sel.Name != "Eq" && sel.Sel.Name != "EqCol") || len(call.Args) != 1 {
			continue
		}
		if isID(sel.X) || isID(call.Args[0]) {
			return true
		}
	}
	return false
}

// fields reports the typed fields in e of a model the query doesn't join
// (allowed are the joins e may name), and Counts a later join repeats.
func (q *queryPass) fields(e ast.Expr, model *types.TypeName, allowed, joins []joined, inJoin bool) {
	info := q.pass.TypesInfo
	ast.Inspect(e, func(n ast.Node) bool {
		x, ok := n.(ast.Expr)
		if !ok {
			return true
		}
		if call, ok := x.(*ast.CallExpr); ok && funcOf(info, call, ormPath) == "Count" && len(call.Args) == 1 {
			q.count(call, joins)
		}
		t := info.TypeOf(x)
		if queriedModel(t) != nil && (named(t, ormPath, "Manager") || named(t, ormPath, "QuerySet") || named(t, ormPath, "Values")) {
			return false // a subquery: a query of its own
		}
		if t == nil || !typedColumn(t) && fieldsModel(t) == nil {
			return true
		}
		tab, ok := q.tableOf(x, 0)
		if !ok {
			return false
		}
		if tab.alias == "" && tab.model == model {
			return false
		}
		for _, j := range allowed {
			if j.t == tab {
				return false
			}
		}
		if inJoin {
			for _, j := range joins {
				if j.t == tab {
					report(q.pass, x, "E0504", "move this Join after the Join of "+tab.String(), "%s is joined after this Join: its ON can't name its columns", tab)
					return false
				}
			}
		}
		report(q.pass, x, "E0504", "join it first: Join("+tab.model.Name()+".On(…)), or read it through a relation of "+model.Name(), "%s isn't joined: the query of %s can't name its fields", tab, model.Name())
		return false
	})
}

// count warns of orm.Count(field) when a join after the field's table
// can repeat its rows.
func (q *queryPass) count(call *ast.CallExpr, joins []joined) {
	tab, ok := q.tableOf(call.Args[0], 0)
	if !ok {
		return
	}
	at := -1
	for i, j := range joins {
		if j.t == tab {
			at = i
		}
	}
	for _, j := range joins[at+1:] {
		if j.repeats {
			report(q.pass, call, "W0501", "count each value once: orm.Count(…, orm.Distinct)",
				"the Join of %s can repeat the rows this Count counts", j.t)
			return
		}
	}
}

// values checks Values[R](cols…) with typed columns against R's fields,
// by position.
func (q *queryPass) values(call *ast.CallExpr, model *types.TypeName, joins []joined) {
	info := q.pass.TypesInfo
	for _, a := range call.Args {
		if _, ok := str(info, a); ok || types.Identical(info.TypeOf(a), types.Typ[types.String]) {
			return
		}
	}
	if call.Ellipsis.IsValid() || len(call.Args) == 0 {
		return
	}
	sig, ok := info.TypeOf(call.Fun).(*types.Signature)
	if !ok || sig.Results().Len() != 1 {
		return
	}
	res, ok := types.Unalias(sig.Results().At(0).Type()).(*types.Named)
	if !ok || res.TypeArgs().Len() != 2 {
		return
	}
	r := res.TypeArgs().At(1)
	st, ok := r.Underlying().(*types.Struct)
	if !ok || is(r, "time", "Time") {
		return
	}
	fields := readFields(st)
	rname := typeString(q.pass, r)
	if len(fields) != len(call.Args) {
		report(q.pass, calledName(call), "E0505", fmt.Sprintf("give %d columns, one per field of %s in order", len(fields), rname),
			"Values[%s] fills its %d fields by position, and %d columns are given", rname, len(fields), len(call.Args))
		return
	}
	for i, a := range call.Args {
		ct := q.columnType(a)
		if ct == nil {
			continue
		}
		if kindOf(ct) != 0 && kindOf(fields[i].Type()) != 0 && kindOf(ct) != kindOf(fields[i].Type()) {
			report(q.pass, a, "E0505", "reorder the columns, or change the field's type",
				"Values[%s] reads column %d, %s, into its field %s, %s", rname, i+1, typeString(q.pass, ct), fields[i].Name(), typeString(q.pass, fields[i].Type()))
		}
	}
}

// readFields is a struct's fields as Values fills them: its own and those
// of the structs it embeds.
func readFields(st *types.Struct) []*types.Var {
	var out []*types.Var
	for i := range st.NumFields() {
		f := st.Field(i)
		if !f.Exported() && !f.Embedded() || strings.Contains(st.Tag(i), `orm:"-"`) {
			continue
		}
		if f.Embedded() {
			if named(f.Type(), ormPath, "Model") || named(f.Type(), ormPath, "Base") {
				continue
			}
			if s, ok := f.Type().Underlying().(*types.Struct); ok {
				out = append(out, readFields(s)...)
				continue
			}
		}
		out = append(out, f)
	}
	return out
}

// columnType is the Go type a Values column reads: a typed field's, an
// aggregate's, a fragment's; nil when it isn't known.
func (q *queryPass) columnType(e ast.Expr) types.Type {
	info := q.pass.TypesInfo
	if call, ok := ast.Unparen(e).(*ast.CallExpr); ok {
		switch funcOf(info, call, ormPath) {
		case "Count":
			return types.Typ[types.Int64]
		case "Avg":
			return types.Typ[types.Float64]
		case "Sum", "Min", "Max":
			if len(call.Args) > 0 {
				return q.columnType(call.Args[0])
			}
		}
	}
	t := info.TypeOf(e)
	if t == nil {
		return nil
	}
	if is(t, ormPath, "TimeField") {
		return lookup(q.pass.Pkg, "time", "Time").Type()
	}
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != ormPath || n.TypeArgs().Len() != 1 {
		return nil
	}
	switch n.Obj().Name() {
	case "Field", "TextField", "NumberField", "SQLExpr":
		return n.TypeArgs().At(0)
	}
	return nil
}

// kindOf is a type's kind as Values reads it: 1 text, 2 a number, 3 a
// time, 0 another.
func kindOf(t types.Type) int {
	for {
		if p, ok := t.(*types.Pointer); ok {
			t = p.Elem()
			continue
		}
		if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == vukaPath && n.Obj().Name() == "Option" && n.TypeArgs().Len() == 1 {
			t = n.TypeArgs().At(0)
			continue
		}
		break
	}
	if is(t, "time", "Time") {
		return 3
	}
	b, ok := t.Underlying().(*types.Basic)
	switch {
	case !ok:
		return 0
	case b.Info()&types.IsString != 0:
		return 1
	case b.Info()&(types.IsInteger|types.IsFloat) != 0:
		return 2
	}
	return 0
}

// fragment checks a SQL fragment: its template (args[0]) a constant, its
// ? and {n} matching the arguments after it.
func (q *queryPass) fragment(call *ast.CallExpr, args []ast.Expr) {
	if len(args) == 0 {
		return
	}
	tmpl, ok := str(q.pass.TypesInfo, args[0])
	if !ok {
		how := "a variable"
		switch x := ast.Unparen(args[0]).(type) {
		case *ast.BinaryExpr:
			how = "+"
		case *ast.CallExpr:
			how = types.ExprString(x.Fun)
		}
		report(q.pass, args[0], "E0506", "write the template as one constant string, and pass values as ? arguments",
			"a SQL fragment's template is built with %s: values reach the database only bound, through ?", how)
		return
	}
	if call.Ellipsis.IsValid() {
		return
	}
	n := len(args) - 1
	marks, refs, err := sqlMarks(tmpl)
	if err != "" {
		report(q.pass, args[0], "E0506", "", "%s", err)
		return
	}
	free := n
	for r := range refs {
		if r >= n {
			report(q.pass, args[0], "E0506", "number the arguments from {0}", "the template's {%d} names no argument: it is given %d", r, n)
			return
		}
		free--
	}
	if marks != free {
		report(q.pass, args[0], "E0506", "give one argument per ?, after those {n} name",
			"the template has %d ? for %d arguments no {n} names", marks, free)
	}
}

// sqlMarks counts a template's ? and the {n} it names, as the orm reads
// them: quoted text is text, ?? and {{ }} literal.
func sqlMarks(tmpl string) (marks int, refs map[int]bool, err string) {
	refs = map[int]bool{}
	for i := 0; i < len(tmpl); i++ {
		c := tmpl[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			end := strings.IndexByte(tmpl[i+1:], c)
			if end < 0 {
				return 0, nil, fmt.Sprintf("the template has an unclosed %c", c)
			}
			i += end + 1
		case (c == '?' || c == '{' || c == '}') && i+1 < len(tmpl) && tmpl[i+1] == c:
			i++
		case c == '?':
			marks++
		case c == '{':
			end := strings.IndexByte(tmpl[i:], '}')
			if end < 0 {
				return 0, nil, "the template has an unclosed {"
			}
			n, e := strconv.Atoi(tmpl[i+1 : i+end])
			if e != nil || n < 0 {
				return 0, nil, fmt.Sprintf("the template's {%s} is no argument number", tmpl[i+1:i+end])
			}
			refs[n] = true
			i += end
		}
	}
	return marks, refs, ""
}
