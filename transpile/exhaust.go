package transpile

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"
)

// space is a pattern as exhaustiveness sees it: a wildcard (ctor ""), or a
// constructor applied to sub-patterns. Ok, Err, Some, None, true, false and a
// struct ("{}", one sub-pattern per field) can together cover a type; any
// other constructor (a literal, a constant, a pin, a type test) is a value of
// its own that never completes one.
type space struct {
	ctor string
	args []*space
}

var anything = &space{}

func wildcards(n int) []*space {
	out := make([]*space, n)
	for i := range out {
		out[i] = anything
	}
	return out
}

// ctorInfo is one of the constructors that together make up a type.
type ctorInfo struct {
	name   string
	args   []types.Type
	fields []string // a struct's field names
}

// ctors are the constructors that cover t, or nil when t's values can't be
// enumerated by patterns.
func ctors(t types.Type) []ctorInfo {
	if t == nil {
		return nil
	}
	switch kind, arg := runtimeType(t); kind {
	case "Result":
		return []ctorInfo{{name: "Ok", args: []types.Type{arg}}, {name: "Err", args: []types.Type{errorType}}}
	case "Option":
		return []ctorInfo{{name: "Some", args: []types.Type{arg}}, {name: "None"}}
	}
	if _, ok := t.(*types.TypeParam); ok {
		return nil
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		if u.Info()&types.IsBoolean != 0 {
			return []ctorInfo{{name: "true"}, {name: "false"}}
		}
	case *types.Struct:
		c := ctorInfo{name: "{}"}
		for i := range u.NumFields() {
			c.args = append(c.args, u.Field(i).Type())
			c.fields = append(c.fields, u.Field(i).Name())
		}
		return []ctorInfo{c}
	}
	return nil
}

// space abstracts a pattern compile accepted, matched against a t.
func (pc *patCtx) space(x ast.Expr, t types.Type) *space {
	kind, arg := runtimeType(t)
	switch x := x.(type) {
	case *ast.ParenExpr:
		return pc.space(x.X, t)
	case *ast.Ident:
		switch {
		case x.Name == "_":
		case x.Name == "None" && kind == "Option":
			return &space{ctor: "None"}
		case pc.isValue(x.Name):
			if b, ok := t.Underlying().(*types.Basic); ok && b.Info()&types.IsBoolean != 0 && (x.Name == "true" || x.Name == "false") {
				if obj, ok := pc.lookup(x.Name).(*types.Const); ok && obj.Parent() == types.Universe {
					return &space{ctor: x.Name}
				}
			}
			return &space{ctor: "=" + x.Name}
		}
		return anything
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok && len(x.Args) == 1 {
			inner := arg
			if id.Name == "Err" {
				inner = errorType
			}
			return &space{ctor: id.Name, args: []*space{pc.space(x.Args[0], inner)}}
		}
	case *ast.CompositeLit:
		if T, err := pc.typeExpr(x.Type); err == nil && types.Identical(T, t) {
			st, ok := t.Underlying().(*types.Struct)
			if !ok {
				break
			}
			s := &space{ctor: "{}", args: wildcards(st.NumFields())}
			for _, el := range x.Elts {
				key := el.(*ast.KeyValueExpr).Key.(*ast.Ident).Name
				i := fieldIndex(st, key)
				if i < 0 { // a promoted field: matched, but not enumerated
					return opaque(x)
				}
				s.args[i] = pc.space(el.(*ast.KeyValueExpr).Value, st.Field(i).Type())
			}
			return s
		}
	}
	return opaque(x)
}

// opaque is a pattern matching values of its own: equal literals and names
// are one value; anything holding a composite literal is unlike any other.
func opaque(x ast.Expr) *space {
	key := types.ExprString(x)
	if strings.Contains(key, "{") {
		key = fmt.Sprintf("%p", x)
	}
	return &space{ctor: "=" + key}
}

func fieldIndex(st *types.Struct, name string) int {
	for i := range st.NumFields() {
		if st.Field(i).Name() == name {
			return i
		}
	}
	return -1
}

// specialize keeps the rows that match constructor name, its sub-patterns
// taking the place of their first column.
func specialize(rows [][]*space, name string, arity int) [][]*space {
	var out [][]*space
	for _, r := range rows {
		switch r[0].ctor {
		case name:
			out = append(out, append(append([]*space(nil), r[0].args...), r[1:]...))
		case "":
			out = append(out, append(wildcards(arity), r[1:]...))
		}
	}
	return out
}

// defaults keeps the rows whose first column matches anything, without it.
func defaults(rows [][]*space) [][]*space {
	var out [][]*space
	for _, r := range rows {
		if r[0].ctor == "" {
			out = append(out, r[1:])
		}
	}
	return out
}

func prepend(ts []types.Type, rest []types.Type) []types.Type {
	return append(append([]types.Type(nil), ts...), rest...)
}

// useful reports whether a value matching v (one pattern per column of ts)
// matches none of rows: a case that isn't is unreachable.
func useful(rows [][]*space, v []*space, ts []types.Type) bool {
	if len(rows) == 0 {
		return true
	}
	if len(ts) == 0 {
		return false
	}
	set := ctors(ts[0])
	if !anyCtor(rows) {
		set = nil
	}
	if h := v[0]; h.ctor != "" {
		var args []types.Type
		for _, c := range set {
			if c.name == h.ctor {
				args = c.args
			}
		}
		if len(args) != len(h.args) { // a value of its own
			args = make([]types.Type, len(h.args))
		}
		return useful(specialize(rows, h.ctor, len(h.args)), append(append([]*space(nil), h.args...), v[1:]...), prepend(args, ts[1:]))
	}
	if set == nil {
		return useful(defaults(rows), v[1:], ts[1:])
	}
	for _, c := range set {
		if useful(specialize(rows, c.name, len(c.args)), append(wildcards(len(c.args)), v[1:]...), prepend(c.args, ts[1:])) {
			return true
		}
	}
	return false
}

// missing lists values (one pattern per column of ts) that no row matches,
// written as patterns.
func (pc *patCtx) missing(rows [][]*space, ts []types.Type) [][]string {
	if len(rows) == 0 {
		w := make([]string, len(ts))
		for i := range w {
			w[i] = "_"
		}
		return [][]string{w}
	}
	if len(ts) == 0 {
		return nil
	}
	set := ctors(ts[0])
	if !anyCtor(rows) {
		set = nil // splitting on it would only multiply the work
	}
	var out [][]string
	if set == nil {
		for _, w := range pc.missing(defaults(rows), ts[1:]) {
			out = append(out, append([]string{"_"}, w...))
		}
		return out
	}
	for _, c := range set {
		for _, w := range pc.missing(specialize(rows, c.name, len(c.args)), prepend(c.args, ts[1:])) {
			out = append(out, append([]string{pc.showCtor(c, ts[0], w[:len(c.args)])}, w[len(c.args):]...))
		}
	}
	return out
}

// showCtor writes constructor c of t with sub-patterns args.
func (pc *patCtx) showCtor(c ctorInfo, t types.Type, args []string) string {
	switch c.name {
	case "Ok", "Err", "Some":
		return c.name + "(" + args[0] + ")"
	case "{}":
		var fs []string
		for i, a := range args {
			if a != "_" {
				fs = append(fs, c.fields[i]+": "+a)
			}
		}
		if len(fs) == 0 {
			return "_"
		}
		return pc.e.display(t) + "{" + strings.Join(fs, ", ") + "}"
	}
	return c.name
}

// exhaustive explains what the unguarded cases rows leave out of t, or is "".
func (pc *patCtx) exhaustive(rows [][]*space, t types.Type) string {
	ws := pc.missing(rows, []types.Type{t})
	if len(ws) == 0 {
		return ""
	}
	if ctors(t) == nil || ws[0][0] == "_" {
		return "only a case that matches anything covers every value"
	}
	var miss []string
	for i, w := range ws {
		if i == 4 {
			miss = append(miss, "…")
			break
		}
		miss = append(miss, w[0])
	}
	return "missing " + strings.Join(miss, " and ")
}

// anyCtor reports whether a row's first column names a constructor.
func anyCtor(rows [][]*space) bool {
	for _, r := range rows {
		if r[0].ctor != "" {
			return true
		}
	}
	return false
}

// catchAll reports whether a row matches every value.
func catchAll(rows [][]*space) bool {
	for _, r := range rows {
		if r[0].ctor == "" {
			return true
		}
	}
	return false
}
