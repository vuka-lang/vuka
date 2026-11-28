package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// The compile firewall (docs/rfc/firewall.md, behind VUKA_FIREWALL=1) keeps
// a body edit of a widely imported package from recompiling its importers.
// Go recompiles a package's direct importers whenever its archive changes,
// but their output comes out the same when its export data did, and the
// cascade stops there. So the watch moves such a package's code, through the
// overlay, to firewallDir below it and leaves at its import path a facade of
// aliases and forwards, whose output stays the same while the package's
// export data does; and it lays the moved code out so that the export data
// does stay the same across body edits: each top-level declaration starts at
// a fixed line of its file, blank lines taking up growth, and the line
// directives outside function bodies keep the values they had (the
// positions the export data records). A declaration outgrowing its place,
// or the file's declarations changing, lays the file out again.
const (
	firewallEnv          = "VUKA_FIREWALL"
	firewallImportersEnv = "VUKA_FIREWALL_IMPORTERS"
	firewallDir          = "0vukafw"
	firewallFacade       = "vukafw_facade.go"
	firewallCore         = "vukafw_"
	firewallSlot         = 20
)

func firewallOn() bool { return os.Getenv(firewallEnv) == "1" }

// fwLayout is how a file was laid out: its declarations' keys, the line
// each starts at, and the values of the directives each declaration's
// positions hang on.
type fwLayout struct {
	keys   []string
	starts []int // starts[0] is the first declaration's; len(keys)+1, the last the file's end
	pins   [][]int
}

var (
	fwMu      sync.Mutex
	fwLayouts = map[string]*fwLayout{} // by the file's path in the source tree
)

// firewall rewrites overlay so that each module package imported by at least
// VUKA_FIREWALL_IMPORTERS (8) of pkgs builds behind a facade. It returns the
// new overlay and the import paths it put behind one.
func firewall(root, modPath, tmp, files, overlay string, pkgs []*load.Package) (string, []string, error) {
	min := 8
	if n, err := strconv.Atoi(os.Getenv(firewallImportersEnv)); err == nil && n > 0 {
		min = n
	}
	importers := map[string]int{}
	for _, p := range pkgs {
		for _, imp := range slices.Compact(slices.Sorted(slices.Values(p.Imports))) {
			if imp != p.ImportPath && (imp == modPath || strings.HasPrefix(imp, modPath+"/")) {
				importers[imp]++
			}
		}
	}
	ov, err := readOverlay(overlay)
	if err != nil {
		return overlay, nil, err
	}
	ctx := overlayContext(ov)
	var walled []string
	fwMu.Lock()
	defer fwMu.Unlock()
	for _, p := range pkgs {
		if p.Name == "main" || importers[p.ImportPath] < min {
			continue
		}
		dir := p.BuildDir()
		bp, err := ctx.ImportDir(dir, 0)
		if err != nil || len(bp.CgoFiles)+len(bp.SFiles)+len(bp.CFiles)+len(bp.HFiles)+len(bp.SysoFiles) > 0 || len(bp.EmbedPatterns) > 0 || len(bp.GoFiles) == 0 {
			traceStep("firewall: left %s: not plain Go", p.ImportPath)
			continue
		}
		srcs := map[string][]byte{}
		for _, name := range bp.GoFiles {
			src, err := readOverlaid(ov, filepath.Join(dir, name))
			if err != nil {
				return overlay, nil, err
			}
			srcs[name] = src
		}
		moved, facade, why := fwPackage(dir, p.ImportPath+"/"+firewallDir, bp.GoFiles, srcs)
		if why != "" {
			traceStep("firewall: left %s: %s", p.ImportPath, why)
			continue
		}
		for _, name := range bp.GoFiles {
			file, err := fwWrite(files, name, moved[name])
			if err != nil {
				return overlay, nil, err
			}
			ov[filepath.Join(dir, name)] = ""
			ov[filepath.Join(dir, firewallDir, name)] = file
		}
		file, err := fwWrite(files, firewallFacade, facade)
		if err != nil {
			return overlay, nil, err
		}
		ov[filepath.Join(dir, firewallFacade)] = file
		walled = append(walled, p.ImportPath)
	}
	if len(walled) == 0 {
		return overlay, nil, nil
	}
	data, err := json.Marshal(struct{ Replace map[string]string }{ov})
	if err != nil {
		return overlay, nil, err
	}
	if overlay == "" {
		overlay = filepath.Join(tmp, "overlay.json")
	}
	return overlay, walled, os.WriteFile(overlay, data, 0o644)
}

func readOverlaid(ov map[string]string, file string) ([]byte, error) {
	if r, ok := ov[file]; ok {
		return os.ReadFile(r)
	}
	return os.ReadFile(file)
}

// fwWrite keeps src in files, named by its content.
func fwWrite(files, name string, src []byte) (string, error) {
	sum := sha256.Sum256(src)
	file := filepath.Join(files, fmt.Sprintf("fw%x_%s", sum[:12], name))
	if _, err := os.Stat(file); err == nil {
		return file, nil
	}
	return file, writeWhole(file, src)
}

// fwPackage is the package in dir, its files srcs, laid out to move to
// corePath, and the facade standing in for it. why says, for a package the
// firewall leaves alone, what keeps it out.
func fwPackage(dir, corePath string, names []string, srcs map[string][]byte) (moved map[string][]byte, facade []byte, why string) {
	fset := token.NewFileSet()
	parsed := map[string]*ast.File{}
	for _, name := range names {
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), srcs[name], parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, nil, "doesn't parse"
		}
		parsed[name] = f
	}
	facade, why = fwFacade(fset, corePath, names, parsed)
	if why != "" {
		return nil, nil, why
	}
	moved = map[string][]byte{}
	for _, name := range names {
		moved[name] = fwLayOut(fset, filepath.Join(dir, name), srcs[name], parsed[name])
	}
	return moved, facade, ""
}

// fwFacade is the Go of a package of the same name re-exporting every
// exported type, constant, function and variable of files from corePath:
// types and constants as aliases, functions as forwards (keeping their names
// for runtime.FuncForPC), or variables bound to them when their signatures
// name what the facade can't. A variable is the core's own, by linkname, so
// that there is one: its type is the declared one or what its initializer
// plainly is, and a function the compiler checks but never runs asserts it.
func fwFacade(fset *token.FileSet, corePath string, names []string, files map[string]*ast.File) ([]byte, string) {
	var pkgName string
	imports := map[string]string{} // name → path, of the packages the facade's declarations name
	type item struct{ name, src string }
	var items []item
	var checks []string
	show := func(n ast.Node) string {
		var b bytes.Buffer
		printer.Fprint(&b, fset, n)
		return b.String()
	}
	importsOf := map[*ast.File]map[string]string{}
	funcs := map[string]*ast.FuncDecl{}
	fileOf := map[*ast.FuncDecl]*ast.File{}
	typeNames := map[string]bool{}
	for _, fname := range names {
		f := files[fname]
		m := map[string]string{}
		for _, is := range f.Imports {
			p, _ := strconv.Unquote(is.Path.Value)
			n := transpile.ImportName(p)
			if is.Name != nil {
				n = is.Name.Name
			}
			m[n] = p
		}
		importsOf[f] = m
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					funcs[d.Name.Name], fileOf[d] = d, f
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					if ts, ok := s.(*ast.TypeSpec); ok {
						typeNames[ts.Name.Name] = true
					}
				}
			}
		}
	}
	// uses says whether exprs, of a file importing fileImports, name only
	// what the facade can: the package's exported names, type parameters,
	// predeclared ones and imported packages', which it adds to imports.
	uses := func(fileImports map[string]string, exprs []ast.Expr, tparams map[string]bool) bool {
		ok := true
		need := map[string]string{}
		for _, e := range exprs {
			ast.Inspect(e, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.SelectorExpr:
					if id, isID := x.X.(*ast.Ident); isID {
						p, imported := fileImports[id.Name]
						if !imported {
							ok = false
						} else if have, dup := imports[id.Name]; dup && have != p {
							ok = false
						} else if have, dup := need[id.Name]; dup && have != p {
							ok = false
						} else {
							need[id.Name] = p
						}
						return false
					}
				case *ast.Ident:
					if !x.IsExported() && !tparams[x.Name] && !predeclared(x.Name) {
						ok = false
					}
				case *ast.FuncLit, *ast.BlockStmt:
					ok = false
				}
				return ok
			})
		}
		if ok {
			for n, p := range need {
				imports[n] = p
			}
		}
		return ok
	}
	// varType is the type of a variable initialized to e in file f, as an
	// expression of the file it is written in, or nil when it isn't plain.
	known := map[string]string{"errors.New": "error", "fmt.Errorf": "error", "time.Date": "time.Time", "time.Now": "time.Time"}
	var varType func(f *ast.File, e ast.Expr) (ast.Expr, map[string]string)
	varType = func(f *ast.File, e ast.Expr) (ast.Expr, map[string]string) {
		for {
			p, ok := e.(*ast.ParenExpr)
			if !ok {
				break
			}
			e = p.X
		}
		switch x := e.(type) {
		case *ast.BasicLit:
			t := map[token.Token]string{token.INT: "int", token.FLOAT: "float64", token.IMAG: "complex128", token.CHAR: "rune", token.STRING: "string"}[x.Kind]
			return ast.NewIdent(t), nil
		case *ast.CompositeLit:
			if a, ok := x.Type.(*ast.ArrayType); x.Type == nil || ok && fwIsEllipsis(a.Len) {
				return nil, nil
			}
			return x.Type, importsOf[f]
		case *ast.UnaryExpr:
			if cl, ok := x.X.(*ast.CompositeLit); ok && x.Op == token.AND {
				if t, im := varType(f, cl); t != nil {
					return &ast.StarExpr{X: t}, im
				}
			}
		case *ast.CallExpr:
			switch fn := x.Fun.(type) {
			case *ast.Ident:
				if typeNames[fn.Name] && len(x.Args) == 1 {
					return fn, importsOf[f]
				}
				d := funcs[fn.Name]
				if d == nil || d.Type.TypeParams != nil || d.Type.Results == nil || len(d.Type.Results.List) != 1 || len(d.Type.Results.List[0].Names) > 1 {
					return nil, nil
				}
				return d.Type.Results.List[0].Type, importsOf[fileOf[d]]
			case *ast.SelectorExpr:
				if pkg, ok := fn.X.(*ast.Ident); ok {
					name := importsOf[f][pkg.Name] + "." + fn.Sel.Name
					if t, ok := known[name]; ok {
						te, _ := parser.ParseExpr(t)
						im := map[string]string{}
						if p, _, ok := strings.Cut(t, "."); ok && !strings.Contains(t, " ") {
							im[p] = p
						}
						return te, im
					}
				}
			}
		}
		return nil, nil
	}
	for _, fname := range names {
		f := files[fname]
		pkgName = f.Name.Name
		fileImports := importsOf[f]
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						if !s.Name.IsExported() {
							continue
						}
						if s.TypeParams == nil {
							items = append(items, item{s.Name.Name, "type " + s.Name.Name + " = " + firewallCore + "." + s.Name.Name})
							continue
						}
						tps := map[string]bool{}
						var args []string
						for _, f := range s.TypeParams.List {
							for _, n := range f.Names {
								tps[n.Name] = true
								args = append(args, n.Name)
							}
						}
						if !uses(fileImports, fieldTypes(s.TypeParams), tps) {
							return nil, "generic type " + s.Name.Name + "'s constraints"
						}
						items = append(items, item{s.Name.Name, "type " + s.Name.Name + tparamList(show, s.TypeParams) + " = " + firewallCore + "." + s.Name.Name + "[" + strings.Join(args, ", ") + "]"})
					case *ast.ValueSpec:
						for i, n := range s.Names {
							if !n.IsExported() {
								continue
							}
							if d.Tok == token.CONST {
								items = append(items, item{n.Name, "const " + n.Name + " = " + firewallCore + "." + n.Name})
								continue
							}
							t, im := s.Type, fileImports
							if t == nil && len(s.Values) == len(s.Names) {
								t, im = varType(f, s.Values[i])
							}
							if t == nil || !fwSymbolSafe(corePath) || !uses(im, []ast.Expr{t}, nil) {
								return nil, "exported variable " + n.Name + " of a type it can't tell"
							}
							ts := show(t)
							items = append(items, item{n.Name, "//go:linkname " + n.Name + " " + corePath + "." + n.Name + "\nvar " + n.Name + " " + ts})
							checks = append(checks, "var _ *"+ts+" = &"+firewallCore+"."+n.Name)
						}
					}
				}
			case *ast.FuncDecl:
				if d.Recv != nil || !d.Name.IsExported() {
					continue
				}
				tps := map[string]bool{}
				var targs []string
				if d.Type.TypeParams != nil {
					for _, f := range d.Type.TypeParams.List {
						for _, n := range f.Names {
							tps[n.Name] = true
							targs = append(targs, n.Name)
						}
					}
				}
				if !uses(fileImports, slices.Concat(fieldTypes(d.Type.TypeParams), fieldTypes(d.Type.Params), fieldTypes(d.Type.Results)), tps) {
					if d.Type.TypeParams != nil {
						return nil, "generic function " + d.Name.Name + "'s signature"
					}
					items = append(items, item{d.Name.Name, "var " + d.Name.Name + " = " + firewallCore + "." + d.Name.Name})
					continue
				}
				items = append(items, item{d.Name.Name, fwForward(show, d.Name.Name, d.Type, targs)})
			}
		}
	}
	slices.SortFunc(items, func(a, b item) int { return strings.Compare(a.name, b.name) })
	var b bytes.Buffer
	b.WriteString("// Code generated by vuka run -watch: the compile firewall's facade. DO NOT EDIT.\n\npackage " + pkgName + "\n\nimport (\n")
	core := firewallCore
	if len(items) == 0 {
		core = "_" // its initialization, all the same
	}
	if len(checks) > 0 {
		b.WriteString("\t_ \"unsafe\"\n")
	}
	b.WriteString("\t" + core + " " + strconv.Quote(corePath) + "\n")
	for _, n := range slices.Sorted(func(yield func(string) bool) {
		for n := range imports {
			if !yield(n) {
				return
			}
		}
	}) {
		b.WriteString("\t" + n + " " + strconv.Quote(imports[n]) + "\n")
	}
	b.WriteString(")\n")
	for _, it := range items {
		b.WriteString("\n" + it.src + "\n")
	}
	if len(checks) > 0 {
		b.WriteString("\nfunc _() {\n\t" + strings.Join(checks, "\n\t") + "\n}\n")
	}
	return b.Bytes(), ""
}

func fwIsEllipsis(e ast.Expr) bool {
	_, ok := e.(*ast.Ellipsis)
	return ok
}

// fwSymbolSafe says whether the linker names path's symbols as it is
// written, which a //go:linkname to them needs.
func fwSymbolSafe(path string) bool {
	return !strings.ContainsFunc(path, func(r rune) bool { return r <= ' ' || r == '%' || r == '"' || r >= 0x7f })
}

func fieldTypes(fl *ast.FieldList) []ast.Expr {
	if fl == nil {
		return nil
	}
	var out []ast.Expr
	for _, f := range fl.List {
		out = append(out, f.Type)
	}
	return out
}

func tparamList(show func(ast.Node) string, fl *ast.FieldList) string {
	var parts []string
	for _, f := range fl.List {
		var names []string
		for _, n := range f.Names {
			names = append(names, n.Name)
		}
		parts = append(parts, strings.Join(names, ", ")+" "+show(f.Type))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// fwForward is a function named name of type ft calling the core's.
func fwForward(show func(ast.Node) string, name string, ft *ast.FuncType, targs []string) string {
	params := &ast.FieldList{}
	var args []string
	variadic := false
	i := 0
	for _, f := range ft.Params.List {
		nf := &ast.Field{Type: f.Type}
		n := max(len(f.Names), 1)
		for range n {
			id := fmt.Sprintf("p%d", i)
			i++
			nf.Names = append(nf.Names, ast.NewIdent(id))
			args = append(args, id)
		}
		if _, ok := f.Type.(*ast.Ellipsis); ok {
			variadic = true
		}
		params.List = append(params.List, nf)
	}
	call := firewallCore + "." + name
	if len(targs) > 0 {
		call += "[" + strings.Join(targs, ", ") + "]"
	}
	call += "(" + strings.Join(args, ", ")
	if variadic {
		call += "..."
	}
	call += ")"
	sig := &ast.FuncType{TypeParams: ft.TypeParams, Params: params, Results: ft.Results}
	body := call
	if ft.Results != nil && len(ft.Results.List) > 0 {
		body = "return " + call
	}
	return "func " + name + strings.TrimPrefix(show(sig), "func") + " { " + body + " }"
}

func predeclared(n string) bool {
	switch n {
	case "bool", "byte", "complex64", "complex128", "error", "float32", "float64", "int", "int8", "int16", "int32", "int64",
		"rune", "string", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "any", "comparable",
		"true", "false", "iota", "nil", "_":
		return true
	}
	return false
}

// fwChunk is a top-level declaration of a file, with the comments before
// it: its lines once laid out but for the blank lines placing it, and the
// values of the directives its exported positions hang on.
type fwChunk struct {
	key   string
	lines []string
	pins  []int    // the values of the directives at pinAt
	pinAt []int    // indexes in lines of the directives pinned
	files []string // their file names
}

// fwLayOut lays file out: each declaration at a line its earlier layout gave
// it while it fits there and the file's declarations are the same.
func fwLayOut(fset *token.FileSet, file string, src []byte, f *ast.File) []byte {
	prologue, chunks := fwChunks(fset, file, src, f)
	keys := make([]string, len(chunks))
	for i, c := range chunks {
		keys[i] = c.key
	}
	old := fwLayouts[file]
	reuse := old != nil && slices.Equal(old.keys, keys) && len(prologue) <= old.starts[0]
	for i := range chunks {
		if !reuse {
			break
		}
		reuse = len(chunks[i].lines) <= old.starts[i+1]-old.starts[i] && len(old.pins[i]) == len(chunks[i].pins)
	}
	lay := &fwLayout{keys: keys}
	if reuse {
		lay.starts = old.starts
		lay.pins = old.pins
	} else {
		at := fwQuantize(len(prologue))
		for _, c := range chunks {
			lay.starts = append(lay.starts, at)
			lay.pins = append(lay.pins, c.pins)
			at += fwQuantize(len(c.lines))
		}
		lay.starts = append(lay.starts, at)
	}
	fwLayouts[file] = lay
	out := slices.Clone(prologue)
	for i, c := range chunks {
		for len(out) < lay.starts[i] {
			out = append(out, "")
		}
		lines := slices.Clone(c.lines)
		for j, at := range c.pinAt {
			lines[at] = "//line " + c.files[j] + ":" + strconv.Itoa(lay.pins[i][j])
		}
		out = append(out, lines...)
	}
	return []byte(strings.Join(out, "\n") + "\n")
}

// fwQuantize is the lines a declaration of n lines is given: at least half a
// slot to grow in, rounded up to a slot.
func fwQuantize(n int) int {
	return (n + firewallSlot/2 + firewallSlot - 1) / firewallSlot * firewallSlot
}

// fwChunks splits file into its prologue (to the end of its imports) and its
// declarations. Each declaration's lines start with a directive giving the
// declaration's position, the directives in its comments dropped; a
// function's body starts with one giving its own lines, which are free to
// change, and the declaration's other directives are the chunk's pins.
func fwChunks(fset *token.FileSet, file string, src []byte, f *ast.File) (prologue []string, chunks []fwChunk) {
	tf := fset.File(f.Pos())
	lines := strings.Split(strings.TrimSuffix(string(src), "\n"), "\n")
	cont := fwContinued(src, len(lines))
	lineOf := func(p token.Pos) int { return tf.PositionFor(p, false).Line - 1 } // 0-based, of the file itself
	isDirective := func(i int) bool { return !cont[i] && strings.HasPrefix(lines[i], "//line ") }
	var decls []ast.Decl
	start := lineOf(f.Name.End()) + 1
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			start = lineOf(d.End()) + 1
			continue
		}
		decls = append(decls, d)
	}
	if len(decls) == 0 {
		return lines, nil
	}
	prologue = lines[:start]
	seen := map[string]int{}
	for k := 0; k < len(decls); k++ {
		d := decls[k]
		end := lineOf(d.End())
		// Declarations sharing a line are one chunk.
		group := []ast.Decl{d}
		for k+1 < len(decls) && lineOf(decls[k+1].Pos()) <= end {
			k++
			group = append(group, decls[k])
			end = max(end, lineOf(decls[k].End()))
		}
		if k == len(decls)-1 {
			end = len(lines) - 1
		}
		head := lineOf(d.Pos())
		key := fwKey(group)
		if n := seen[key]; n > 0 {
			key += "#" + strconv.Itoa(n)
		}
		seen[key]++
		var c fwChunk
		c.key = key
		pos := fset.PositionFor(d.Pos(), true)
		var pre []string
		for i := start; i < head; i++ {
			if !isDirective(i) {
				pre = append(pre, lines[i])
			}
		}
		headLine := pos.Line - len(pre)
		if headLine >= 1 {
			c.pinAt = append(c.pinAt, 0)
			c.lines = append([]string{""}, pre...)
		} else {
			c.lines = append(c.lines, pre...)
			c.pinAt = append(c.pinAt, len(c.lines))
			c.lines = append(c.lines, "")
			headLine = pos.Line
		}
		c.pins = append(c.pins, headLine)
		c.files = append(c.files, pos.Filename)
		var bodies [][2]int // 0-based lines of function bodies' braces
		for _, g := range group {
			if fd, ok := g.(*ast.FuncDecl); ok && fd.Body != nil {
				bodies = append(bodies, [2]int{lineOf(fd.Body.Lbrace), lineOf(fd.Body.Rbrace)})
			}
		}
		inBody := func(i int) bool {
			return slices.ContainsFunc(bodies, func(b [2]int) bool { return i > b[0] && i < b[1] })
		}
		for i := head; i <= end; i++ {
			if isDirective(i) && !inBody(i) {
				if p, ok := fwDirective(lines[i]); ok {
					c.pinAt = append(c.pinAt, len(c.lines))
					c.pins = append(c.pins, p.Line)
					c.files = append(c.files, p.Filename)
				}
			}
			c.lines = append(c.lines, lines[i])
			if slices.ContainsFunc(bodies, func(b [2]int) bool { return b[0] == i && b[1] > i }) && i+1 < len(lines) && !cont[i+1] && !isDirective(i+1) {
				p := fset.PositionFor(tf.LineStart(i+2), true)
				c.lines = append(c.lines, "//line "+p.Filename+":"+strconv.Itoa(p.Line))
			}
		}
		chunks = append(chunks, c)
		start = end + 1
	}
	return prologue, chunks
}

// fwDirective is the position a //line directive gives.
func fwDirective(line string) (token.Position, bool) {
	s := strings.TrimPrefix(line, "//line ")
	i := strings.LastIndexByte(s, ':')
	if i < 0 {
		return token.Position{}, false
	}
	n, err := strconv.Atoi(s[i+1:])
	if err != nil {
		// file:line:col
		j := strings.LastIndexByte(s[:i], ':')
		if j < 0 {
			return token.Position{}, false
		}
		if n, err = strconv.Atoi(s[j+1 : i]); err != nil {
			return token.Position{}, false
		}
		i = j
	}
	return token.Position{Filename: s[:i], Line: n}, true
}

// fwContinued says, for each line of src, whether it continues a token
// starting on an earlier one (a raw string, a block comment): no line can
// go before it.
func fwContinued(src []byte, n int) []bool {
	cont := make([]bool, n+1)
	fset := token.NewFileSet()
	tf := fset.AddFile("", -1, len(src))
	var sc scanner.Scanner
	sc.Init(tf, src, nil, scanner.ScanComments)
	for {
		pos, tok, lit := sc.Scan()
		if tok == token.EOF {
			return cont
		}
		if tok != token.STRING && tok != token.COMMENT {
			continue
		}
		first := tf.PositionFor(pos, false).Line - 1
		last := first + strings.Count(lit, "\n")
		for i := first + 1; i <= last && i < len(cont); i++ {
			cont[i] = true
		}
	}
}

// fwKey names a chunk's declarations, the same from one layout to the next.
func fwKey(group []ast.Decl) string {
	var parts []string
	for _, d := range group {
		switch d := d.(type) {
		case *ast.FuncDecl:
			k := "func "
			if d.Recv != nil && len(d.Recv.List) > 0 {
				k += fwRecv(d.Recv.List[0].Type) + "."
			}
			parts = append(parts, k+d.Name.Name)
		case *ast.GenDecl:
			var names []string
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					names = append(names, s.Name.Name)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						names = append(names, n.Name)
					}
				}
			}
			parts = append(parts, d.Tok.String()+" "+strings.Join(names, ","))
		}
	}
	return strings.Join(parts, ";")
}

// fwRecv is the receiver type's name.
func fwRecv(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.StarExpr:
		return fwRecv(e.X)
	case *ast.IndexExpr:
		return fwRecv(e.X)
	case *ast.IndexListExpr:
		return fwRecv(e.X)
	case *ast.Ident:
		return e.Name
	}
	return "?"
}
