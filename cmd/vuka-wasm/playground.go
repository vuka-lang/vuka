package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	goformat "go/format"
	"go/importer"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/format"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// modulePath is the module a playground project is: its folders are the
// module's packages, main.vuka at the root its main.
const modulePath = "example.com/play"

// knownMessage says what a project may import.
const knownMessage = "the playground knows std, ui, web and orm"

// playground transpiles projects against a types snapshot.
type playground struct {
	pkgs map[string][]byte
	gc   types.ImporterFrom
}

func newPlayground(pkgs map[string][]byte) *playground {
	pg := &playground{pkgs: pkgs}
	pg.gc = importer.ForCompiler(token.NewFileSet(), "gc", pg.lookup).(types.ImporterFrom)
	return pg
}

func (pg *playground) lookup(path string) (io.ReadCloser, error) {
	data, ok := pg.pkgs[path]
	if !ok {
		return nil, fmt.Errorf("%s: %s", path, knownMessage)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// result is a transpiled project: the Go of each source file, by the
// file's path, and every diagnostic, as vuka build and vet report them.
type result struct {
	Files       map[string]string `json:"files"`
	Diagnostics []diag.Diagnostic `json:"diagnostics"`
}

// transpile transpiles and type-checks every package of the project files
// (path → source).
func (pg *playground) transpile(files map[string]string) result {
	r := &run{pg: pg, files: files, dirs: map[string]string{}, names: map[string][]string{},
		state: map[string]*pkgState{}, out: result{Files: map[string]string{}, Diagnostics: []diag.Diagnostic{}}}
	var paths []string
	for p := range files {
		if bad := badPath(p); bad != "" {
			r.out.Diagnostics = append(r.out.Diagnostics, diag.Diagnostic{Code: "E0003", Message: bad, Span: diag.Span{File: p}})
			continue
		}
		if !source(p) {
			continue
		}
		dir := path.Dir(p)
		if dir == "." {
			dir = ""
		}
		ip := importPath(dir)
		if _, ok := r.dirs[ip]; !ok {
			paths = append(paths, ip)
		}
		r.dirs[ip] = dir
		r.names[dir] = append(r.names[dir], path.Base(p))
	}
	for _, ns := range r.names {
		sort.Strings(ns)
	}
	sort.Strings(paths)
	for _, ip := range paths {
		r.check(ip)
	}
	// A file failing only because of an import comes last, as vuka build
	// reports it.
	sort.SliceStable(r.out.Diagnostics, func(i, j int) bool {
		ki, kj := r.out.Diagnostics[i].Code == "E0005", r.out.Diagnostics[j].Code == "E0005"
		if ki != kj {
			return kj
		}
		a, b := r.out.Diagnostics[i].Span, r.out.Diagnostics[j].Span
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Col < b.Col
	})
	return r.out
}

// source reports whether p is a file a package is made of.
func source(p string) bool {
	return strings.HasSuffix(p, ".vuka") || strings.HasSuffix(p, ".vuka.html") ||
		strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go")
}

// badPath says what is wrong with a project file's path; "" when nothing.
func badPath(p string) string {
	switch {
	case p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\"):
		return fmt.Sprintf("%q isn't a path in the project", p)
	case path.Clean(p) != p || strings.HasPrefix(p, "../"):
		return fmt.Sprintf("%q isn't a clean path in the project", p)
	}
	return ""
}

func importPath(dir string) string {
	if dir == "" {
		return modulePath
	}
	return modulePath + "/" + dir
}

// run is one transpile of a project.
type run struct {
	pg    *playground
	files map[string]string
	dirs  map[string]string   // import path → folder
	names map[string][]string // folder → its files' names
	state map[string]*pkgState
	out   result
}

type pkgState struct {
	busy bool
	pkg  *types.Package
	err  error
}

func (r *run) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if _, ok := r.dirs[path]; ok {
		return r.check(path)
	}
	if strings.HasPrefix(path, modulePath+"/") {
		return nil, fmt.Errorf("the project has no folder %s", strings.TrimPrefix(path, modulePath+"/"))
	}
	return r.pg.gc.ImportFrom(path, "", 0)
}

// check transpiles and type-checks the project's package ip, once.
func (r *run) check(ip string) (*types.Package, error) {
	if st := r.state[ip]; st != nil {
		if st.busy {
			return nil, fmt.Errorf("import cycle through %s", ip)
		}
		return st.pkg, st.err
	}
	st := &pkgState{busy: true}
	r.state[ip] = st
	st.pkg, st.err = r.compile(ip)
	st.busy = false
	return st.pkg, st.err
}

func (r *run) compile(ip string) (*types.Package, error) {
	dir := r.dirs[ip]
	join := func(name string) string { return path.Join(dir, name) }
	var files, goFiles []transpile.File
	for _, name := range r.names[dir] {
		f := transpile.File{Name: name, Src: []byte(r.files[join(name)])}
		if f.IsVuka() {
			files = append(files, f)
		} else {
			goFiles = append(goFiles, f)
		}
	}
	broken := fmt.Errorf("%s has errors", ip)
	res, err := transpile.Package(append(files, goFiles...), transpile.Options{Importer: r, Path: join, ImportPath: ip})
	if err != nil {
		r.report(err)
		return nil, broken
	}
	// The Go files are parsed by their base names, so that the line
	// directives' paths, relative to a file's folder, stay the project's.
	fset := token.NewFileSet()
	var asts []*ast.File
	gens := map[string]generated{}
	span := func(p token.Position) diag.Span {
		if !strings.Contains(p.Filename, "/") && p.Filename != "" {
			p.Filename = join(p.Filename)
		}
		return diag.Span{File: p.Filename, Line: p.Line, Col: p.Column}
	}
	parse := func(name string, src []byte) bool {
		f, err := parser.ParseFile(fset, name, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			var list scanner.ErrorList
			if errors.As(err, &list) {
				for _, e := range list {
					r.add(diag.Diagnostic{Code: "E0003", Message: e.Msg, Span: span(e.Pos)})
				}
			} else {
				r.add(diag.Diagnostic{Code: "E0003", Message: err.Error(), Span: diag.Span{File: join(name)}})
			}
			return false
		}
		asts = append(asts, f)
		return true
	}
	ok := true
	for _, o := range res.Files {
		gen := o.GoName
		r.out.Files[join(o.Name)] = string(o.Src)
		gens[gen] = generated{path: join(o.Name), src: r.files[join(o.Name)], m: o.Map}
		for _, w := range o.Warnings {
			d := w.Diagnostic("")
			d.Severity = diag.Warning
			r.add(d)
		}
		ok = parse(gen, o.Src) && ok
	}
	if res.Calls != nil {
		r.out.Files[join(transpile.CallsGoName)] = string(res.Calls)
		ok = parse(transpile.CallsGoName, res.Calls) && ok
	}
	for _, f := range goFiles {
		ok = parse(f.Name, f.Src) && ok
	}
	if !ok {
		return nil, broken
	}
	if stub := routesStub(dir, asts); stub != nil {
		parse("routes.go", stub)
	}
	failed := false
	conf := types.Config{Importer: r, Error: func(err error) {
		var te types.Error
		if !errors.As(err, &te) {
			r.add(diag.Diagnostic{Code: "E0002", Message: err.Error()})
			failed = true
			return
		}
		d := diag.Diagnostic{Code: "E0002", Message: te.Msg, Span: span(te.Fset.Position(te.Pos))}
		if g, ok := gens[te.Fset.PositionFor(te.Pos, false).Filename]; ok && g.m != nil {
			if off, _, ok := g.m.ToSource(te.Fset.PositionFor(te.Pos, false).Offset); ok && off <= len(g.src) {
				d.Span.File = g.path
				d.Span.Line, d.Span.Col = lineCol(g.src, off)
			}
			d.Message = g.m.Message(d.Span.Line, d.Message)
		}
		r.add(r.importError(d))
		failed = true
	}}
	pkg, _ := conf.Check(ip, fset, asts, nil)
	if failed {
		return pkg, broken
	}
	return pkg, nil
}

// generated is a Go file a source file became.
type generated struct {
	path, src string
	m         *transpile.SourceMap
}

// lineCol is the 1-based line and byte column of off in src.
func lineCol(src string, off int) (int, int) {
	before := src[:off]
	line := strings.Count(before, "\n") + 1
	return line, off - strings.LastIndexByte(before, '\n')
}

// importError is d said as vuka build says it when it is an import that
// failed: a project package with errors of its own (E0005), or a package
// the playground hasn't.
func (r *run) importError(d diag.Diagnostic) diag.Diagnostic {
	rest, ok := strings.CutPrefix(d.Message, "could not import ")
	if !ok {
		return d
	}
	path, _, _ := strings.Cut(rest, " ")
	if src, ok := r.files[d.Span.File]; ok && d.Span.Line > 0 && d.Span.Col == 0 {
		lines := strings.Split(src, "\n")
		if d.Span.Line <= len(lines) {
			if i := strings.Index(lines[d.Span.Line-1], strconv.Quote(path)); i >= 0 {
				d.Span.Col, d.Span.EndCol = i+1, i+1+len(strconv.Quote(path))
			}
		}
	}
	switch {
	case r.dirs[path] != "" || path == modulePath:
		d.Code, d.Message = "E0005", path+" has errors"
		d.Help = "fix its errors first"
	case r.pg.pkgs[path] == nil && !strings.HasPrefix(path, modulePath+"/"):
		d.Message = "cannot import " + path + ": " + knownMessage
		if first, _, _ := strings.Cut(path, "/"); !strings.Contains(first, ".") {
			d.Message = "cannot import " + path + ": of std, the playground knows the packages apps commonly import; it knows ui, web and orm too"
		}
	}
	return d
}

// routesStub declares Routes in the project's pages package when nothing
// does: the vuka command writes it from the folders under pages/, which the
// playground doesn't.
func routesStub(dir string, files []*ast.File) []byte {
	if dir != "pages" || len(files) == 0 {
		return nil
	}
	for _, f := range files {
		for _, d := range f.Decls {
			if fn, ok := d.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.Name == "Routes" {
				return nil
			}
		}
	}
	return []byte("package " + files[0].Name.Name + "\n\nimport \"github.com/vuka-lang/web/v2\"\n\nfunc Routes() web.Option { return web.OptionFunc(func(*web.App) {}) }\n")
}

func (r *run) add(d diag.Diagnostic) { r.out.Diagnostics = append(r.out.Diagnostics, d) }

// report adds what a transpile returned.
func (r *run) report(err error) {
	ds, rest := diag.From(err)
	for _, d := range ds {
		r.add(r.importError(d))
	}
	for _, e := range rest {
		r.add(diag.Diagnostic{Code: "E0001", Message: e.Error()})
	}
}

// formatted is a formatted file, or why it isn't.
type formatted struct {
	Src   string           `json:"src,omitempty"`
	Error *diag.Diagnostic `json:"error,omitempty"`
}

// formatFile formats one file as vuka fmt does.
func formatFile(p, src string) formatted {
	var out []byte
	var err error
	switch {
	case strings.HasSuffix(p, ".vuka.html"):
		pkg := path.Base(path.Dir(p))
		if pkg == "." {
			pkg = "main"
		}
		out, err = format.Template([]byte(src), pkg)
	case strings.HasSuffix(p, ".vuka"):
		out, err = format.Source([]byte(src))
	case strings.HasSuffix(p, ".go"):
		out, err = goformat.Source([]byte(src))
	default:
		return formatted{Src: src}
	}
	if err != nil {
		return formatted{Error: formatError(p, err)}
	}
	return formatted{Src: string(out)}
}

// formatError is a formatter's error as a diagnostic: its message starts
// line:col: as format's do, or is a scanner's list.
func formatError(p string, err error) *diag.Diagnostic {
	d := &diag.Diagnostic{Code: "E0003", Span: diag.Span{File: p}, Message: err.Error()}
	var list scanner.ErrorList
	if errors.As(err, &list) && len(list) > 0 {
		d.Message, d.Span.Line, d.Span.Col = list[0].Msg, list[0].Pos.Line, list[0].Pos.Column
		return d
	}
	msg := strings.TrimPrefix(d.Message, ":")
	if parts := strings.SplitN(msg, ":", 3); len(parts) == 3 {
		l, e1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		c, e2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if e1 == nil && e2 == nil {
			d.Message, d.Span.Line, d.Span.Col = strings.TrimSpace(parts[2]), l, c
		}
	}
	return d
}
