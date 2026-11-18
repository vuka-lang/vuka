package transpile

import (
	"go/ast"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

// loadsWrites warns of a @ui.Loads method assigning a field of its receiver
// that isn't deferred to the method's group (W0353): the deferred render
// ships only the group's fields, so the page never sees the value. Only
// assignments in the method's own body count.
func (e *engine) loadsWrites() {
	names := make([]string, 0, len(e.schemas))
	for name := range e.schemas {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		si := e.liveSchema(name)
		if si == nil {
			continue
		}
		fields := e.deferredFields(name)
		for _, h := range si.hooks {
			if lastName(h.Name) != "Loads" {
				continue
			}
			group, err := strconv.Unquote(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(h.Args, "("), ")")))
			if err != nil || group == "" {
				continue
			}
			e.loadsWritesIn(si, h, group, fields)
		}
	}
}

func (e *engine) loadsWritesIn(si *schemaInfo, h *Attr, group string, fields map[string]string) {
	obj, _ := e.pkg.Scope().Lookup(si.typeName).(*types.TypeName)
	if obj == nil {
		return
	}
	sel, _, _ := types.LookupFieldOrMethod(types.NewPointer(obj.Type()), true, e.pkg, h.hookMethod)
	m, _ := sel.(*types.Func)
	if m == nil {
		return
	}
	fd := e.methodDecl(m)
	if fd == nil || fd.Body == nil || fd.Recv == nil || len(fd.Recv.List) == 0 || len(fd.Recv.List[0].Names) == 0 {
		return
	}
	recv := e.info.Defs[fd.Recv.List[0].Names[0]]
	var f *fileState
	for _, c := range e.vuka {
		if c.ast == nil {
			continue
		}
		for _, d := range c.ast.Decls {
			if d == ast.Decl(fd) {
				f = c
			}
		}
	}
	if recv == nil || f == nil {
		return
	}
	seen := map[string]bool{}
	written := func(x ast.Expr) {
		s, ok := ast.Unparen(x).(*ast.SelectorExpr)
		if !ok || seen[s.Sel.Name] {
			return
		}
		id, ok := ast.Unparen(s.X).(*ast.Ident)
		if !ok || e.info.Uses[id] != recv {
			return
		}
		field, ok := e.info.Uses[s.Sel].(*types.Var)
		if !ok || !field.IsField() {
			return
		}
		if g, deferred := fields[s.Sel.Name]; deferred && groupName(g) == groupName(group) {
			return
		}
		seen[s.Sel.Name] = true
		q, _, _ := strings.Cut(h.Name, ".")
		f.susWarnings.add("W0353", f.at(f.orig(s.Pos())), "`%s` is set in `%s` (`@%s.Loads(%q)`) but isn't `@%s.Defer(%q)`; the page won't see the value", s.Sel.Name, m.Name(), q, group, q, group).
			help("mark it @%s.Defer(%q), or set it in Mount", q, group)
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			for _, l := range s.Lhs {
				written(l)
			}
		case *ast.IncDecStmt:
			written(s.X)
		case *ast.CallExpr:
			if sel, ok := ast.Unparen(s.Fun).(*ast.SelectorExpr); ok && (sel.Sel.Name == "Set" || sel.Sel.Name == "Update") {
				if t := e.info.TypeOf(sel.X); t != nil {
					if n, ok := types.Unalias(t).(*types.Named); ok && n.Obj().Name() == "Assign" {
						written(sel.X)
					}
				}
			}
		}
		return true
	})
}
