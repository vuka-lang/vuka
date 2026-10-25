package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/toolchain/tsprops"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// Islands of plain Go: what the vuka command reads of a .go file, as it
// reads @ui.Island of a .vuka one — ui.RegisterIsland[T](args…), a route's
// ui.Island(args…) option (the island is the page its constructor returns),
// and ui.Island(args…)(vuka.TypeFor[T](…)), the form older vuka commands
// generated. The arguments are read as @ui.Island's are.

// goPkgs parses a module's Go packages once, by directory.
type goPkgs struct {
	root, modPath string
	fset          *token.FileSet
	dirs          map[string][]*goFile
}

type goFile struct {
	path string
	src  []byte
	ast  *ast.File
}

// pkg is the parsed non-test .go files of dir.
func (g *goPkgs) pkg(dir string) []*goFile {
	if fs, ok := g.dirs[dir]; ok {
		return fs
	}
	var fs []*goFile
	entries, _ := load.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		p := filepath.Join(dir, n)
		if src, err := load.ReadSource(p); err == nil {
			if f, err := parser.ParseFile(g.fset, p, src, parser.SkipObjectResolution); err == nil {
				fs = append(fs, &goFile{p, src, f})
			}
		}
	}
	g.dirs[dir] = fs
	return fs
}

// importDir is the directory of the module's package a file imports as
// name; "" when it imports none of the module's by that name.
func (g *goPkgs) importDir(f *goFile, name string) string {
	for _, im := range f.ast.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		n := transpile.ImportName(p)
		if im.Name != nil {
			n = im.Name.Name
		}
		if n != name {
			continue
		}
		if p == g.modPath {
			return g.root
		}
		if rest, ok := strings.CutPrefix(p, g.modPath+"/"); ok {
			return filepath.Join(g.root, filepath.FromSlash(rest))
		}
	}
	return ""
}

// typeDecl finds the struct type e names, read in f: its package's file
// declaring it and its name, and the package's directory.
func (g *goPkgs) typeDecl(f *goFile, e ast.Expr) (*goFile, string, string) {
	dir, name := filepath.Dir(f.path), ""
	switch x := e.(type) {
	case *ast.StarExpr:
		return g.typeDecl(f, x.X)
	case *ast.Ident:
		name = x.Name
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		if !ok {
			return nil, "", ""
		}
		if dir = g.importDir(f, id.Name); dir == "" {
			return nil, "", ""
		}
		name = x.Sel.Name
	default:
		return nil, "", ""
	}
	for _, df := range g.pkg(dir) {
		for _, d := range df.ast.Decls {
			if gd, ok := d.(*ast.GenDecl); ok && gd.Tok == token.TYPE {
				for _, s := range gd.Specs {
					if ts := s.(*ast.TypeSpec); ts.Name.Name == name {
						if _, ok := ts.Type.(*ast.StructType); ok {
							return df, name, dir
						}
					}
				}
			}
		}
	}
	return nil, "", dir
}

// ctorType is the type the constructor e (NewBoard, pages.NewBoard)
// returns, as typeDecl finds it.
func (g *goPkgs) ctorType(f *goFile, e ast.Expr) (*goFile, string, string) {
	dir, name := filepath.Dir(f.path), ""
	switch x := e.(type) {
	case *ast.Ident:
		name = x.Name
	case *ast.SelectorExpr:
		id, ok := x.X.(*ast.Ident)
		if !ok {
			return nil, "", ""
		}
		if dir = g.importDir(f, id.Name); dir == "" {
			return nil, "", ""
		}
		name = x.Sel.Name
	default:
		return nil, "", ""
	}
	for _, df := range g.pkg(dir) {
		for _, d := range df.ast.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == name && fd.Type.Results != nil && len(fd.Type.Results.List) > 0 {
				return g.typeDecl(df, fd.Type.Results.List[0].Type)
			}
		}
	}
	return nil, "", dir
}

// readGoIslands reads the islands a .go file registers into pa.
func (pa *pageAssets) readGoIslands(g *goPkgs, file string) []error {
	var f *goFile
	for _, pf := range g.pkg(filepath.Dir(file)) {
		if pf.path == file {
			f = pf
		}
	}
	if f == nil {
		return nil
	}
	uiName := importName(f.ast, load.UIPath)
	if uiName == "" {
		return nil
	}
	isUI := func(e ast.Expr, sel string) bool {
		s, ok := e.(*ast.SelectorExpr)
		if !ok || s.Sel.Name != sel {
			return false
		}
		id, ok := s.X.(*ast.Ident)
		return ok && id.Name == uiName
	}
	typeArg := func(e ast.Expr) (ast.Expr, ast.Expr) {
		switch x := e.(type) {
		case *ast.IndexExpr:
			return x.X, x.Index
		case *ast.IndexListExpr:
			return x.X, x.Indices[0]
		}
		return nil, nil
	}
	var errs []error
	done := map[*ast.CallExpr]bool{}
	add := func(call *ast.CallExpr, how string, args []ast.Expr, df *goFile, name, dir, route string) {
		pos := g.fset.Position(call.Pos())
		rel, _ := filepath.Rel(pa.root, file)
		where := fmt.Sprintf("%s:%d", filepath.ToSlash(rel), pos.Line)
		if df == nil && hasVuka(dir) {
			return // declared in Vuka, read from there
		}
		if df == nil {
			errs = append(errs, diag.New("E0320", "%s: the island's type isn't a struct of the module the vuka command can find", how).At(labelSpan(where)))
			return
		}
		ic, bad := islandArgs(how, uiName, args)
		if bad != "" {
			errs = append(errs, diag.New("E0320", "%s", bad).At(labelSpan(where)))
			return
		}
		full, _ := scanFile(df.path, df.src)
		typeRel, _ := filepath.Rel(pa.root, df.path)
		ic.name, ic.label, ic.namedBy, ic.route, ic.dir = name, where, how, route, filepath.Dir(df.path)
		ic.props = structFields(full, name)
		ic.live = embeds(full, name, importName(df.ast, load.UIPath), "Live")
		ic.locateArg(pa.root, filepath.Dir(file), filepath.ToSlash(typeRel))
		pa.islands = append(pa.islands, ic)
	}
	ast.Inspect(f.ast, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || done[call] {
			return true
		}
		if fn, t := typeArg(call.Fun); fn != nil && isUI(fn, "RegisterIsland") {
			df, name, dir := g.typeDecl(f, t)
			add(call, uiName+".RegisterIsland["+exprName(t)+"]", call.Args, df, name, dir, "")
			return true
		}
		if inner, ok := call.Fun.(*ast.CallExpr); ok && isUI(inner.Fun, "Island") && len(call.Args) == 1 {
			if desc, ok := call.Args[0].(*ast.CallExpr); ok {
				if fn, t := typeArg(desc.Fun); fn != nil {
					done[inner] = true
					df, name, dir := g.typeDecl(f, t)
					add(inner, uiName+".Island", inner.Args, df, name, dir, "")
					return true
				}
			}
		}
		route := ""
		if s, ok := call.Fun.(*ast.SelectorExpr); ok {
			route = s.Sel.Name
		}
		for i, a := range call.Args {
			opt, ok := a.(*ast.CallExpr)
			if !ok || i < 2 || !isUI(opt.Fun, "Island") {
				continue
			}
			done[opt] = true
			// The page is the route's handler: its constructor, the argument
			// before the options.
			var df *goFile
			var name, dir string
			for j := i - 1; j >= 1 && df == nil; j-- {
				var d string
				if df, name, d = g.ctorType(f, call.Args[j]); dir == "" {
					dir = d
				}
			}
			if df != nil || routeVerbs[route] {
				add(opt, uiName+".Island", opt.Args, df, name, dir, route)
			}
		}
		return true
	})
	return errs
}

// routeVerbs are the registry methods taking a page's constructor.
var routeVerbs = map[string]bool{"Get": true, "Post": true, "Put": true, "Patch": true, "Delete": true, "Handle": true, "Live": true}

// islandArgs reads an island's arguments as @ui.Island's are: a string
// literal path, then a strategy, then options, ui.Strategy("…") read and
// ui.Layout(…) and other calls left to the app. bad says what is wrong.
func islandArgs(how, uiName string, args []ast.Expr) (ic *islandComp, bad string) {
	ic = &islandComp{}
	str := func(e ast.Expr) (string, bool) {
		if l, ok := e.(*ast.BasicLit); ok && l.Kind == token.STRING {
			v, err := strconv.Unquote(l.Value)
			return v, err == nil
		}
		return "", false
	}
	strs := 0
	for _, a := range args {
		if v, ok := str(a); ok {
			switch strs {
			case 0:
				ic.src = v
			case 1:
				ic.strategy = v
			}
			strs++
			continue
		}
		c, ok := a.(*ast.CallExpr)
		if !ok {
			if strs == 0 {
				return nil, fmt.Sprintf("%s takes a string literal path (the vuka command reads it from the source)", how)
			}
			return nil, fmt.Sprintf("%s's options are calls: %s.Layout(…), %s.Strategy(\"…\")", how, uiName, uiName)
		}
		s, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if id, ok := s.X.(*ast.Ident); !ok || id.Name != uiName {
			continue
		}
		switch s.Sel.Name {
		case "Strategy":
			v, ok := "", len(c.Args) == 1
			if ok {
				v, ok = str(c.Args[0])
			}
			if !ok {
				return nil, fmt.Sprintf("%s.Strategy takes a string literal (the vuka command reads it from the source)", uiName)
			}
			ic.strategy = v
		case "Layout":
			ic.layout = true
		}
	}
	return ic, ""
}

// locateArg sets where the component of an island whose path argument is
// in ic.src is: inferred from the type's file (typeRel) when there is none,
// else resolved against dir, the directory of the file naming it.
func (ic *islandComp) locateArg(root, dir, typeRel string) {
	cp := ic.src
	if cp == "" {
		ic.infer, ic.module = true, islandModule(typeRel)
		return
	}
	abs, err := tsprops.ResolveIslandPath(root, dir, cp)
	if err != nil {
		ic.problem, ic.problemCode = "can't read island component "+cp+": "+err.Error(), "E0303"
		return
	}
	if strings.HasPrefix(cp, "@/") {
		if _, _, err := tsprops.ReadIslandComponent(root, dir, cp, load.ReadSource); err != nil {
			ic.problem, ic.problemCode = err.Error(), "E0303"
		}
	}
	relSrc, _ := filepath.Rel(root, abs)
	if !strings.HasPrefix(cp, ".") && !strings.HasPrefix(cp, "@/") && !filepath.IsAbs(cp) {
		ic.bare = cp
	}
	ic.src, ic.rel, ic.framework = abs, filepath.ToSlash(relSrc), islandFramework(cp)
}

// hasVuka reports whether dir holds .vuka files.
func hasVuka(dir string) bool {
	if dir == "" {
		return false
	}
	m, _ := filepath.Glob(filepath.Join(dir, "*.vuka"))
	return len(m) > 0
}

// importName is the name a file imports path as; "" when it doesn't.
func importName(f *ast.File, path string) string {
	for _, im := range f.Imports {
		if p, _ := strconv.Unquote(im.Path.Value); p == path {
			if im.Name != nil {
				return im.Name.Name
			}
			return transpile.ImportName(path)
		}
	}
	return ""
}

// exprName is a type expression's source form, for messages.
func exprName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprName(x.X) + "." + x.Sel.Name
	case *ast.StarExpr:
		return "*" + exprName(x.X)
	}
	return "T"
}
