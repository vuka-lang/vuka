package main

import (
	"go/ast"
	"go/build"
	"go/types"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"

	"github.com/vuka-lang/vuka/v2/diag"
)

// urlParams checks the components a page's markup renders: a field of one
// marked with a bare @ui.URL binds the route's path parameter of its name,
// so the page's path must have it (E0340).
func (c *routeChecker) urlParams() {
	bare := map[*types.TypeName][]string{}
	for _, rt := range c.res.routes {
		if rt.typ == "" || rt.Kind != "live" && rt.Kind != "page" || rt.dir == nil {
			continue
		}
		syn, ok := c.r.syntax[rt.dir.importPath]
		if !ok {
			continue
		}
		params := pathParams(rt.Path)
		for _, f := range syn.files {
			for _, d := range f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Body == nil || fd.Recv == nil || len(fd.Recv.List) == 0 || recvTypeName(fd.Recv.List[0].Type) != rt.typ ||
					fd.Name.Name != "Render" && fd.Name.Name != "View" {
					continue
				}
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					lit, ok := n.(*ast.CompositeLit)
					if !ok {
						return true
					}
					tn := c.litType(syn, lit.Type)
					if tn == nil {
						return true
					}
					fields, seen := bare[tn]
					if !seen {
						fields = c.bareURLFields(tn)
						bare[tn] = fields
					}
					for _, name := range fields {
						if matchFold(params, name) != "" {
							continue
						}
						d := diag.New("E0340", "%s.%s binds the path parameter {%s} (a bare @ui.URL), and %s's path %s has none", tn.Name(), name, name, rt.typ, rt.Path)
						if s := closest(name, params); s != "" {
							d = d.WithHelp("did you mean {%s}? name the field %s, or give @ui.URL the parameter: @ui.URL(\"%s\") reads the query", s, s, name)
						} else if len(params) > 0 {
							d = d.WithHelp("the path's parameters are {%s}; or read the query: @ui.URL(\"%s\")", strings.Join(params, "}, {"), name)
						} else {
							d = d.WithHelp("render it under a [%s] folder, or read the query: @ui.URL(\"%s\")", name, name)
						}
						c.report(d, lit.Pos())
					}
					return true
				})
			}
		}
	}
}

// litType is the struct type a composite literal's type names, T or
// pkg.T.
func (c *routeChecker) litType(syn checked, x ast.Expr) *types.TypeName {
	var id *ast.Ident
	switch t := x.(type) {
	case *ast.Ident:
		id = t
	case *ast.SelectorExpr:
		id = t.Sel
	case *ast.IndexExpr:
		return c.litType(syn, t.X)
	}
	if id == nil {
		return nil
	}
	tn, _ := syn.uses[id].(*types.TypeName)
	return tn
}

func recvTypeName(x ast.Expr) string {
	if s, ok := x.(*ast.StarExpr); ok {
		x = s.X
	}
	if ix, ok := x.(*ast.IndexExpr); ok {
		x = ix.X
	}
	if id, ok := x.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// pathParams are the names of a route path's {name} segments.
func pathParams(path string) []string {
	var out []string
	for _, seg := range strings.Split(path, "/") {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			out = append(out, strings.TrimSuffix(seg[1:len(seg)-1], "..."))
		}
	}
	return out
}

func matchFold(names []string, name string) string {
	for _, n := range names {
		if strings.EqualFold(n, name) {
			return n
		}
	}
	return ""
}

// bareURLFields are the fields of tn's struct that its Vuka source marks
// with a bare @ui.URL: read from the .vuka and .vuka.html files of its
// package's directory.
func (c *routeChecker) bareURLFields(tn *types.TypeName) []string {
	if tn.Pkg() == nil {
		return nil
	}
	dir := ""
	switch path := tn.Pkg().Path(); {
	case path == c.t.modPath:
		dir = c.t.root
	case strings.HasPrefix(path, c.t.modPath+"/"):
		dir = filepath.Join(c.t.root, filepath.FromSlash(strings.TrimPrefix(path, c.t.modPath+"/")))
	default:
		if bp, err := build.Default.Import(path, c.t.root, build.FindOnly); err == nil {
			dir = bp.Dir
		}
	}
	if d := c.r.byPath[tn.Pkg().Path()]; d != nil {
		dir = d.dir
	}
	if dir == "" {
		return nil
	}
	entries, _ := load.ReadDir(dir)
	decl := regexp.MustCompile(`\btype\s+` + regexp.QuoteMeta(tn.Name()) + `\s+struct\s*\{`)
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".vuka") && !strings.HasSuffix(n, ".vuka.html") {
			continue
		}
		src, err := load.ReadSource(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		loc := decl.FindIndex(src)
		if loc == nil {
			continue
		}
		depth, end := 0, len(src)
		for i := loc[1] - 1; i < len(src); i++ {
			if src[i] == '{' {
				depth++
			} else if src[i] == '}' {
				if depth--; depth == 0 {
					end = i
					break
				}
			}
		}
		var out []string
		for _, a := range urlAttrs(src[loc[1]:end]) {
			if a.bare {
				out = append(out, a.field)
			}
		}
		return out
	}
	return nil
}
