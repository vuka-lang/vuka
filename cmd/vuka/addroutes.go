package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// routesEdit is what serving a pages/ tree takes from the package main
// that owns it: the change to one of its files that passes the tree's
// Routes() to the app, or, when vuka can't make it, the line to add by
// hand.
type routesEdit struct {
	File   string // the main's file to change, relative to the module root ("" when no main owns the tree)
	Src    []byte // its new source; nil when Served or Hint
	Served bool   // a main passes the tree's Routes() to its app already
	Hint   string // what to add, and where, when it can't be edited
}

// pagesRoutesEdit is the routesEdit for the pages/ directory pagesDir
// (absolute) of the module at root. Its owner follows the folder routes'
// rule: a pages/ directory beside a package main is that main's; the
// module's own pages/ is the module root's main when the root is one, else
// the module's only main. Several mains and none passing it are left to the
// author: the hint names them.
func pagesRoutesEdit(root, modPath, pagesDir string) routesEdit {
	root = realPathOf(root)
	rel, err := filepath.Rel(root, filepath.Join(realPathOf(filepath.Dir(pagesDir)), filepath.Base(pagesDir)))
	if err != nil || strings.HasPrefix(rel, "..") {
		return routesEdit{}
	}
	rel = filepath.ToSlash(rel)
	pagesPath := modPath + "/" + rel
	pkg := pagePackage(root, rel)
	line := pkg + ".Routes()"

	owner := filepath.Dir(filepath.Join(root, rel))
	mains := [][]string{mainFiles(owner)}
	if rel == pagesDirName && len(mains[0]) == 0 {
		mains = nil
		walkSources(root, func(path, name string) error {
			if dir := filepath.Dir(path); isSourceFile(name) && !slices.ContainsFunc(mains, func(fs []string) bool { return filepath.Dir(fs[0]) == dir }) {
				if fs := mainFiles(dir); len(fs) > 0 {
					mains = append(mains, fs)
				}
			}
			return nil
		})
	}
	if len(mains) == 0 || len(mains[0]) == 0 {
		return routesEdit{}
	}
	for _, fs := range mains {
		for _, f := range fs {
			if src, err := os.ReadFile(f); err == nil && passesRoutes(src, pagesPath, pkg) {
				return routesEdit{File: relPath(root, f), Served: true}
			}
		}
	}
	// hint is the line to add by hand to file (or where), and why vuka
	// didn't add it.
	hint := func(file, why string) routesEdit {
		return routesEdit{File: file, Hint: fmt.Sprintf("%s/ is served by no app yet (%s): add %s to the app's web.New in %s, importing %q:\n\n\tweb.New(%s, …)",
			rel, why, line, file, pagesPath, line)}
	}
	if len(mains) > 1 {
		var files []string
		for _, fs := range mains {
			files = append(files, relPath(root, fs[0]))
		}
		sort.Strings(files)
		r := hint("the main of the app that serves it", "the module has several package mains, "+strings.Join(files, ", ")+", and none passes "+line)
		r.File = ""
		return r
	}

	type site struct {
		file string
		src  []byte
		f    *ast.File
		call *ast.CallExpr
	}
	var sites []site
	parsed := map[string]*ast.File{}
	srcs := map[string][]byte{}
	fset := token.NewFileSet()
	for _, f := range mains[0] {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		file, err := parser.ParseFile(fset, f, maskVuka(src), parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			if bytes.Contains(src, []byte(".New(")) && bytes.Contains(src, []byte(strconv.Quote(webPath))) {
				return hint(relPath(root, f), "vuka can't read "+filepath.Base(f)+" to edit it")
			}
			continue
		}
		parsed[f], srcs[f] = file, src
		web := importName(file, webPath)
		if web == "" || web == "_" || web == "." {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok && isSel(c.Fun, web, "New") {
				sites = append(sites, site{f, src, file, c})
			}
			return true
		})
	}
	mainFile := relPath(root, mains[0][0])
	if len(sites) != 1 {
		why := "its main has no web.New call vuka can find"
		if len(sites) > 1 {
			why, mainFile = "its main calls web.New more than once", relPath(root, sites[0].file)
		}
		return hint(mainFile, why)
	}
	s := sites[0]
	file := relPath(root, s.file)
	off := func(p token.Pos) int { return fset.Position(p).Offset }

	// The edit lands in the file of the option list: the call's arguments,
	// or the slice literal a spread argument names in the same file.
	var ins spliceEdit
	switch {
	case !s.call.Ellipsis.IsValid():
		at := off(s.call.Lparen) + 1
		switch {
		case len(s.call.Args) == 0:
			ins = spliceEdit{at, at, "PAGES.Routes()"}
		case bytes.Contains(s.src[at:off(s.call.Args[0].Pos())], []byte("\n")): // one option a line
			at = bytes.LastIndexByte(s.src[:off(s.call.Args[0].Pos())], '\n') + 1
			ins = spliceEdit{at, at, "PAGES.Routes(),\n"}
		default:
			ins = spliceEdit{at, at, "PAGES.Routes(), "}
		}
	default:
		lit := spreadLiteral(s.f, s.call)
		if lit == nil {
			return hint(file, "its web.New takes a list of options vuka can't follow")
		}
		ins = literalInsert(lit, off, s.src, "// the pages under "+rel+"/")
	}

	pkgName, edits := pkg, []spliceEdit{ins}
	switch spec := importSpecOf(s.f, pagesPath); {
	case spec == nil:
		if nameTaken(s.f, pkg) {
			return hint(file, "the name "+pkg+" is taken in "+filepath.Base(s.file))
		}
		edits = append(edits, importInsert(s.f, off, s.src, pagesPath))
	case spec.Name == nil:
	case spec.Name.Name == "_":
		if nameTaken(s.f, pkg) {
			return hint(file, "the name "+pkg+" is taken in "+filepath.Base(s.file))
		}
		edits = append(edits, spliceEdit{off(spec.Name.Pos()), off(spec.Path.Pos()), ""})
	case spec.Name.Name == ".":
		return hint(file, "it imports "+pagesPath+" with a dot")
	default:
		pkgName = spec.Name.Name
	}
	edits[0].text = strings.ReplaceAll(edits[0].text, "PAGES", pkgName)
	out := spliceEdits(s.src, edits)
	b, err := formatSource(filepath.Base(s.file), out, modPath)
	if err != nil {
		return hint(file, "the edited file doesn't format")
	}
	return routesEdit{File: file, Src: b}
}

// servePages makes the main that owns the module's pages/ pass its
// Routes() to the app, after vuka add wrote a page there, and says so on w;
// when it can't, it prints the line to add.
func servePages(root, modPath string, w io.Writer) error {
	e := pagesRoutesEdit(root, modPath, filepath.Join(root, pagesDirName))
	switch {
	case e.Src != nil:
		if err := os.WriteFile(filepath.Join(root, e.File), e.Src, 0o644); err != nil {
			return err
		}
		fmt.Fprintln(w, "changed", filepath.ToSlash(e.File))
	case e.Hint != "":
		fmt.Fprintf(w, "\n%s\n", e.Hint)
	}
	return nil
}

// fixRoutes is vuka fix routes: W0701's fix, for each page tree with pages
// that no main serves and whose owner vuka can edit.
func fixRoutes(m *fixModule) ([]fixChange, error) {
	var changes []fixChange
	for _, t := range scanRoutes(m.root, m.modPath, nil) {
		if !slices.ContainsFunc(t.dirs, func(d *routeDir) bool { return len(d.pages) > 0 || len(d.funcs) > 0 }) {
			continue
		}
		e := pagesRoutesEdit(m.root, m.modPath, t.pages)
		if e.Src == nil {
			continue
		}
		path, src := filepath.Join(m.root, e.File), e.Src
		changes = append(changes, fixChange{
			what:  fmt.Sprintf("pass %s.Routes() to web.New in %s", pagePackage(m.root, filepath.ToSlash(relPath(m.root, t.pages))), filepath.ToSlash(e.File)),
			apply: func() error { return os.WriteFile(path, src, 0o644) },
		})
	}
	return changes, nil
}

// spliceEdit replaces src[from:to] with text.
type spliceEdit struct {
	from, to int
	text     string
}

func spliceEdits(src []byte, edits []spliceEdit) []byte {
	slices.SortFunc(edits, func(a, b spliceEdit) int { return b.from - a.from })
	out := slices.Clone(src)
	for _, e := range edits {
		out = slices.Concat(out[:e.from], []byte(e.text), out[e.to:])
	}
	return out
}

// mainFiles are the package main's files in dir, generated ones left out:
// none when dir holds no package main.
func mainFiles(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !isSourceFile(n) {
			continue
		}
		if src, err := os.ReadFile(filepath.Join(dir, n)); err == nil && mainPkg.Match(src) && !bytes.HasPrefix(src, []byte("// Code generated")) {
			out = append(out, filepath.Join(dir, n))
		}
	}
	return out
}

// isSourceFile reports whether name is a hand-written Go or Vuka file other
// than a test.
func isSourceFile(name string) bool {
	return (strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".vuka")) && !strings.Contains(name, "_test.") &&
		!strings.HasSuffix(name, "_gen.go") && !strings.HasSuffix(name, "_vuka.go") && !strings.HasSuffix(name, "_html.go") && !strings.HasSuffix(name, "_templ.go")
}

// passesRoutes reports whether src, a main's file, imports pagesPath (the
// package pkg) and calls its Routes().
func passesRoutes(src []byte, pagesPath, pkg string) bool {
	importRe := regexp.MustCompile(`(?m)^\s*(?:import\s+)?(\w+)?\s*` + regexp.QuoteMeta(strconv.Quote(pagesPath)))
	for _, m := range importRe.FindAllSubmatch(src, -1) {
		alias := string(m[1])
		if alias == "" {
			alias = pkg
		}
		if bytes.Contains(src, []byte(alias+".Routes()")) {
			return true
		}
	}
	return false
}

// maskVuka blanks what go/parser can't read in a .vuka file, keeping every
// offset: a line's leading @decorator and the ? of an error return.
func maskVuka(src []byte) []byte {
	out := slices.Clone(src)
	var s scanner.Scanner
	fset := token.NewFileSet()
	s.Init(fset.AddFile("", -1, len(src)), src, func(token.Position, string) {}, 0)
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			return out
		}
		if tok != token.ILLEGAL {
			continue
		}
		i := fset.Position(pos).Offset
		switch lit {
		case "?":
			out[i] = ' '
		case "@":
			start := bytes.LastIndexByte(src[:i], '\n') + 1
			if len(bytes.TrimSpace(src[start:i])) > 0 {
				continue
			}
			end := bytes.IndexByte(src[i:], '\n')
			if end < 0 {
				end = len(src) - i
			}
			for j := i; j < i+end; j++ {
				out[j] = ' '
			}
		}
	}
}

func importSpecOf(file *ast.File, path string) *ast.ImportSpec {
	for _, s := range file.Imports {
		if p, _ := strconv.Unquote(s.Path.Value); p == path {
			return s
		}
	}
	return nil
}

// nameTaken reports whether name is an import's name or a top-level
// declaration in file.
func nameTaken(file *ast.File, name string) bool {
	for _, s := range file.Imports {
		p, _ := strconv.Unquote(s.Path.Value)
		if s.Name != nil && s.Name.Name == name || s.Name == nil && filepath.Base(p) == name {
			return true
		}
	}
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == name {
				return true
			}
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.ValueSpec:
					if slices.ContainsFunc(s.Names, func(n *ast.Ident) bool { return n.Name == name }) {
						return true
					}
				case *ast.TypeSpec:
					if s.Name.Name == name {
						return true
					}
				}
			}
		}
	}
	return false
}

// spreadLiteral is the slice literal the spread argument of call stands
// for, when file declares it: the one result of a function called without
// arguments (web.New(options()...)), or a variable defined once by one
// (opts := []web.Option{…}; web.New(opts...)).
func spreadLiteral(file *ast.File, call *ast.CallExpr) *ast.CompositeLit {
	if len(call.Args) != 1 {
		return nil
	}
	sliceLit := func(e ast.Expr) *ast.CompositeLit {
		if l, ok := e.(*ast.CompositeLit); ok {
			if _, ok := l.Type.(*ast.ArrayType); ok {
				return l
			}
		}
		return nil
	}
	switch a := call.Args[0].(type) {
	case *ast.CallExpr:
		fn, ok := a.Fun.(*ast.Ident)
		if !ok || len(a.Args) > 0 {
			return nil
		}
		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || fd.Name.Name != fn.Name || fd.Body == nil {
				continue
			}
			var lits []*ast.CompositeLit
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if _, ok := n.(*ast.FuncLit); ok {
					return false
				}
				if r, ok := n.(*ast.ReturnStmt); ok {
					if len(r.Results) == 1 {
						lits = append(lits, sliceLit(r.Results[0]))
					} else {
						lits = append(lits, nil)
					}
				}
				return true
			})
			if len(lits) == 1 {
				return lits[0]
			}
		}
	case *ast.Ident:
		var found []*ast.CompositeLit
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.AssignStmt:
				if n.Tok == token.DEFINE && len(n.Lhs) == len(n.Rhs) {
					for i, l := range n.Lhs {
						if id, ok := l.(*ast.Ident); ok && id.Name == a.Name {
							found = append(found, sliceLit(n.Rhs[i]))
						}
					}
				}
			case *ast.ValueSpec:
				for i, id := range n.Names {
					if id.Name == a.Name && i < len(n.Values) {
						found = append(found, sliceLit(n.Values[i]))
					}
				}
			}
			return true
		})
		if len(found) == 1 {
			return found[0]
		}
	}
	return nil
}

// literalInsert adds PAGES.Routes() as the last element of lit: on a line
// of its own before the closing brace when the literal spans lines.
func literalInsert(lit *ast.CompositeLit, off func(token.Pos) int, src []byte, comment string) spliceEdit {
	rb := off(lit.Rbrace)
	if len(lit.Elts) == 0 {
		return spliceEdit{rb, rb, "PAGES.Routes()"}
	}
	last := off(lit.Elts[len(lit.Elts)-1].End())
	if !bytes.Contains(src[last:rb], []byte("\n")) {
		return spliceEdit{last, last, ", PAGES.Routes()"}
	}
	at := bytes.LastIndexByte(src[:rb], '\n') + 1
	return spliceEdit{at, at, "PAGES.Routes(), " + comment + "\n"}
}

// importInsert adds path to file's imports: in its last import block, else
// after its last import declaration.
func importInsert(file *ast.File, off func(token.Pos) int, src []byte, path string) spliceEdit {
	var last *ast.GenDecl
	for _, d := range file.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			last = g
		}
	}
	q := strconv.Quote(path)
	if last == nil {
		at := off(file.Name.End())
		return spliceEdit{at, at, "\n\nimport " + q}
	}
	if last.Rparen.IsValid() {
		rp := off(last.Rparen)
		at := bytes.LastIndexByte(src[:rp], '\n') + 1
		if len(bytes.TrimSpace(src[at:rp])) > 0 || at <= off(last.Lparen) {
			return spliceEdit{rp, rp, "\n\t" + q + "\n"}
		}
		return spliceEdit{at, at, "\t" + q + "\n"}
	}
	at := off(last.End())
	return spliceEdit{at, at, "\nimport " + q}
}
