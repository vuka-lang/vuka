package check

import (
	"go/ast"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// URLState checks the URL state of the components a markup renders: two
// instances keeping one query parameter (a field marked @ui.URL("page")
// each) without a urlKey of their own write over each other's (W0307).
var URLState = &analysis.Analyzer{
	Name:      "urlstate",
	Doc:       "check that the components a page renders keep URL parameters of their own",
	URL:       docsURL,
	Run:       runURLState,
	FactTypes: []analysis.Fact{new(urlKeysFact)},
}

// urlKeysFact is the query parameters a component type's LiveSchema keeps
// its fields in.
type urlKeysFact struct{ Keys []string }

func (*urlKeysFact) AFact()           {}
func (f *urlKeysFact) String() string { return "urlKeys(" + strings.Join(f.Keys, ",") + ")" }

func runURLState(pass *analysis.Pass) (any, error) {
	info := pass.TypesInfo
	local := map[*types.TypeName][]string{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Recv == nil || fd.Name.Name != "LiveSchema" {
				continue
			}
			tn := recvTypeName(info, fd)
			if tn == nil {
				continue
			}
			if keys := schemaURLKeys(info, fd.Body); len(keys) > 0 {
				local[tn] = keys
				pass.ExportObjectFact(tn, &urlKeysFact{keys})
			}
		}
	}
	keysOf := func(tn *types.TypeName) []string {
		if k, ok := local[tn]; ok {
			return k
		}
		var f urlKeysFact
		if pass.ImportObjectFact(tn, &f) {
			return f.Keys
		}
		return nil
	}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Recv == nil || fd.Name.Name != "Render" && fd.Name.Name != "View" {
				continue
			}
			checkURLKeys(pass, fd, keysOf)
		}
	}
	return nil, nil
}

// recvTypeName is the named type of a method's receiver.
func recvTypeName(info *types.Info, fd *ast.FuncDecl) *types.TypeName {
	t := fd.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	if ix, ok := t.(*ast.IndexExpr); ok {
		t = ix.X
	}
	id, ok := t.(*ast.Ident)
	if !ok {
		return nil
	}
	tn, _ := info.Uses[id].(*types.TypeName)
	return tn
}

// schemaURLKeys are the query parameters a LiveSchema's l.Field calls give
// fields, ui.URL("page") or ui.URL("page").Default(…): a bare ui.URL (a
// path parameter) keeps none.
func schemaURLKeys(info *types.Info, body *ast.BlockStmt) []string {
	var keys []string
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if m, _ := method(info, call, uiPath, "LiveSchema"); m != "Field" {
			return true
		}
		for _, a := range call.Args[1:] {
			x := ast.Unparen(a)
			if c, ok := x.(*ast.CallExpr); ok {
				if m, recv := method(info, c, uiPath, "URL"); m == "Default" {
					x = ast.Unparen(recv)
				}
			}
			c, ok := x.(*ast.CallExpr)
			if !ok || len(c.Args) != 1 {
				continue
			}
			if tv, ok := info.Types[c.Fun]; !ok || !tv.IsType() || !is(tv.Type, uiPath, "URL") {
				continue
			}
			if k, ok := str(info, c.Args[0]); ok && k != "" {
				keys = append(keys, k)
			}
		}
		return true
	})
	sort.Strings(keys)
	return keys
}

// checkURLKeys reports two component literals in a render keeping one
// parameter: the same key under the same ui.URLKey (or none).
func checkURLKeys(pass *analysis.Pass, fd *ast.FuncDecl, keysOf func(*types.TypeName) []string) {
	info := pass.TypesInfo
	prefix := map[*ast.CompositeLit]string{} // a literal a ui.URLKey names: its key; "\x00" not constant
	var lit func(x ast.Expr) *ast.CompositeLit
	lit = func(x ast.Expr) *ast.CompositeLit {
		x = ast.Unparen(x)
		if u, ok := x.(*ast.UnaryExpr); ok {
			x = ast.Unparen(u.X)
		}
		switch x := x.(type) {
		case *ast.CompositeLit:
			return x
		case *ast.CallExpr:
			if funcOf(info, x, uiPath) == "Key" && len(x.Args) == 2 {
				return lit(x.Args[1])
			}
		}
		return nil
	}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || funcOf(info, call, uiPath) != "URLKey" || len(call.Args) != 2 {
			return true
		}
		if l := lit(call.Args[1]); l != nil {
			k, ok := str(info, call.Args[0])
			if !ok {
				k = "\x00"
			}
			prefix[l] = k
		}
		return true
	})
	type seen struct {
		typ string
		at  *ast.CompositeLit
	}
	first := map[string]seen{}
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		tv, ok := info.Types[cl]
		if !ok {
			return true
		}
		nt, ok := types.Unalias(tv.Type).(*types.Named)
		if !ok {
			return true
		}
		p := prefix[cl]
		if p == "\x00" {
			return true // a key computed as it renders: one per instance, presumably
		}
		for _, k := range keysOf(nt.Obj()) {
			full := k
			if p != "" {
				full = p + "." + k
			}
			if s, ok := first[full]; ok {
				where := ""
				if p != "" {
					where = " (urlKey " + `"` + p + `")`
				}
				report(pass, cl, "W0307", "give each tag a urlKey of its own: <"+nt.Obj().Name()+` urlKey="…"/>`,
					"%s%s and the %s before it both keep the query parameter %s: each writes over the other's", nt.Obj().Name(), where, s.typ, full)
				continue
			}
			first[full] = seen{nt.Obj().Name(), cl}
		}
		return true
	})
}
