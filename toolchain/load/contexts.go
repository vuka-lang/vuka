package load

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// modPkg is a package of the module, as finding the context processors
// reads it: its name and imports.
type modPkg struct {
	dir, path, name string
	imports         []string
	mainFiles       []string // a package main's .go and .vuka files
}

// Contexts are the context processors each package of pkgs reads by name,
// by import path: those every package main of the module at root linking
// it in lists in web.ContextProcessors(…). A main listing none is left
// out: it renders no page reading them. read reads a source (the editor's
// buffer); nil reads the disk.
func Contexts(root, modPath string, pkgs []*Package, read ReadFunc) map[string][]transpile.Context {
	if read == nil {
		read = os.ReadFile
	}
	root = realPath(root)
	mod := scanModule(root, modPath, read)
	for _, p := range pkgs {
		// The packages being transpiled are as their files are now.
		path := strings.TrimSuffix(p.ImportPath, "_test")
		if m := mod[path]; m != nil && !strings.HasSuffix(p.ImportPath, "_test") {
			m.imports = slices.Clone(p.Imports)
		}
	}
	var mains []transpile.Context
	reach := map[string][]int{} // import path → the mains linking it in
	for _, m := range sortedPkgs(mod) {
		if m.name != "main" {
			continue
		}
		c, ok := mainContext(m, read)
		if !ok {
			continue
		}
		i := len(mains)
		mains = append(mains, c)
		seen := map[string]bool{}
		var visit func(path string)
		visit = func(path string) {
			q := mod[path]
			if q == nil || seen[path] {
				return
			}
			seen[path] = true
			reach[path] = append(reach[path], i)
			for _, imp := range q.imports {
				visit(imp)
			}
		}
		visit(m.path)
	}
	if len(mains) == 0 {
		return nil
	}
	out := map[string][]transpile.Context{}
	for _, p := range pkgs {
		for _, i := range reach[strings.TrimSuffix(p.ImportPath, "_test")] {
			out[p.ImportPath] = append(out[p.ImportPath], mains[i])
		}
	}
	return out
}

// sortedPkgs are mod's packages by import path.
func sortedPkgs(mod map[string]*modPkg) []*modPkg {
	out := make([]*modPkg, 0, len(mod))
	for _, m := range mod {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

// scanModule is every package of the module at root, by import path: the
// names and imports of their files, Go, Vuka and template.
func scanModule(root, modPath string, read ReadFunc) map[string]*modPkg {
	mod := map[string]*modPkg{}
	filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		if err != nil || !e.IsDir() {
			return nil
		}
		if path != root {
			n := e.Name()
			if strings.HasPrefix(n, ".") || strings.HasPrefix(n, "_") && !inPages(root, path) || n == "testdata" || n == "vendor" || n == "node_modules" || IsGoName(path) {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			}
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		ip := modPath
		if rel != "." {
			ip += "/" + GoRel(filepath.ToSlash(rel))
		}
		var m *modPkg
		for _, f := range entries {
			n := f.Name()
			if f.IsDir() || strings.Contains(n, "_test.") {
				continue
			}
			var name string
			var imps []string
			switch {
			case strings.HasSuffix(n, ".vuka.html"):
				src, err := read(filepath.Join(path, n))
				if err != nil {
					continue
				}
				head := transpile.ParseTemplateHead(src)
				name = head.Package
				for _, imp := range head.Imports {
					imps = append(imps, imp.Path)
				}
			case strings.HasSuffix(n, ".go") || strings.HasSuffix(n, ".vuka"):
				if strings.HasSuffix(n, ".go") && isGenerated(entries, n) {
					continue
				}
				src, err := read(filepath.Join(path, n))
				if err != nil {
					continue
				}
				af, _ := parser.ParseFile(token.NewFileSet(), n, src, parser.ImportsOnly)
				if af == nil || af.Name == nil {
					continue
				}
				name = af.Name.Name
				for _, imp := range af.Imports {
					if p, err := strconv.Unquote(imp.Path.Value); err == nil {
						imps = append(imps, p)
					}
				}
				if name == "main" {
					if m == nil {
						m = &modPkg{dir: path, path: ip, name: name}
					}
					m.mainFiles = append(m.mainFiles, filepath.Join(path, n))
				}
			default:
				continue
			}
			if name == "" || strings.HasSuffix(name, "_test") {
				continue
			}
			if m == nil {
				m = &modPkg{dir: path, path: ip, name: name}
			}
			if m.name == "" {
				m.name = name
			}
			m.imports = append(m.imports, imps...)
		}
		if m != nil {
			mod[ip] = m
		}
		return nil
	})
	return mod
}

// isGenerated reports whether the .go file name is a Vuka source's output
// beside it.
func isGenerated(entries []fs.DirEntry, name string) bool {
	for _, e := range entries {
		if n := e.Name(); (strings.HasSuffix(n, ".vuka") || strings.HasSuffix(n, ".vuka.html")) && transpile.GoName(n) == name {
			return true
		}
	}
	return name == transpile.CallsGoName
}

// mainContext is the context processors a package main lists, and whether
// it lists any: the arguments of each web.ContextProcessors(…) call in its
// files.
func mainContext(m *modPkg, read ReadFunc) (transpile.Context, bool) {
	c := transpile.Context{Main: m.path, Dir: m.dir}
	found := false
	for _, file := range m.mainFiles {
		src, err := read(file)
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		af, _ := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
		if af == nil {
			continue
		}
		names := importNames(af)
		ast.Inspect(af, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || !isWebCall(call, names, "ContextProcessors") {
				return true
			}
			found = true
			for _, a := range call.Args {
				if p, ok := processorOf(a, names, m.path, false, false, false); ok {
					p.Pos, p.End = fset.Position(a.Pos()), fset.Position(a.End())
					c.Processors = append(c.Processors, p)
				}
			}
			return true
		})
	}
	return c, found
}

// importNames are a file's imports by the name it uses for each.
func importNames(af *ast.File) map[string]string {
	names := map[string]string{}
	for _, imp := range af.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		n := transpile.PackageName(p)
		if imp.Name != nil {
			n = imp.Name.Name
		}
		names[n] = p
	}
	return names
}

// isWebCall reports whether call calls web's function name.
func isWebCall(call *ast.CallExpr, names map[string]string, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	return ok && names[x.Name] == transpile.WebPath
}

// processorOf is the processor an entry of the list names: users.Context,
// a function of main, or one wrapped in web.Browser, web.Personal or
// web.Auth.
func processorOf(x ast.Expr, names map[string]string, main string, browser, personal, auth bool) (transpile.Processor, bool) {
	switch x := ast.Unparen(x).(type) {
	case *ast.Ident:
		return transpile.Processor{Path: main, Func: x.Name, Browser: browser, Personal: personal, Auth: auth}, true
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok && names[id.Name] != "" {
			return transpile.Processor{Path: names[id.Name], Func: x.Sel.Name, Browser: browser, Personal: personal, Auth: auth}, true
		}
	case *ast.CallExpr:
		if len(x.Args) == 0 {
			return transpile.Processor{}, false
		}
		switch {
		case isWebCall(x, names, "Browser"):
			return processorOf(x.Args[0], names, main, true, personal, auth)
		case isWebCall(x, names, "Personal"):
			return processorOf(x.Args[0], names, main, browser, true, auth)
		case isWebCall(x, names, "Auth"):
			return processorOf(x.Args[0], names, main, browser, personal, true)
		}
	}
	return transpile.Processor{}, false
}

// processorPaths are the packages of cs' processors.
func processorPaths(cs []transpile.Context) []string {
	var out []string
	for _, c := range cs {
		for _, p := range c.Processors {
			if !slices.Contains(out, p.Path) {
				out = append(out, p.Path)
			}
		}
	}
	sort.Strings(out)
	return out
}
