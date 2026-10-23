package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// Moving a shape's methods out of its header, into the logic file beside
// it: x.vuka.html's into x.vuka, a .vuka file of the same package. The
// compiler lowers tracked state there as in the header (2.2), so a method
// reads and writes its fields as written wherever it is. Methods stay in
// the header that the shape's kind depends on: its decorated hooks, the
// Title a page names itself with, and, for a component that is live only
// because its markup sends an event, the exported methods the markup
// sends. The same move takes a method out of a .go file, where tracked
// state doesn't compile (E0155).

// headerDecl is a top-level declaration of Go source read tolerantly: a
// .vuka.html header, or a .vuka or .go file.
type headerDecl struct {
	kind       token.Token // FUNC, TYPE, VAR or CONST
	start      int         // its first line's start: its doc comment and decorators included
	declStart  int         // its keyword
	end        int         // the end of its last line, the newline included
	decorators []string
	name       string
	recvBase   string
	bodyStart  int // a func's body: the offsets of its { and }; 0 without one
	bodyEnd    int
}

// headerDecls reads the top-level declarations of src[from:to].
func headerDecls(src []byte, from, to int) []headerDecl {
	text := src[from:to]
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(text))
	var sc scanner.Scanner
	sc.Init(file, text, func(token.Position, string) {}, 0)
	type tok struct {
		t   token.Token
		off int
		lit string
	}
	var toks []tok
	for {
		p, t, lit := sc.Scan()
		if t == token.EOF {
			break
		}
		toks = append(toks, tok{t, from + file.Offset(p), lit})
	}
	lineEnd := func(off int) int {
		if i := bytes.IndexByte(src[off:], '\n'); i >= 0 {
			return off + i + 1
		}
		return len(src)
	}
	lineStart := func(off int) int { return bytes.LastIndexByte(src[:off], '\n') + 1 }
	var out []headerDecl
	depth, decoStart := 0, -1
	var decos []string
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch t.t {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth++
			continue
		case token.RPAREN, token.RBRACK, token.RBRACE:
			depth--
			continue
		}
		if depth != 0 {
			continue
		}
		if t.t == token.ILLEGAL && t.lit == "@" {
			if decoStart < 0 {
				decoStart = t.off
			}
			end := lineEnd(t.off)
			decos = append(decos, strings.TrimSpace(string(src[t.off:end])))
			for i+1 < len(toks) && toks[i+1].off < end {
				i++
				switch toks[i].t {
				case token.LPAREN, token.LBRACK, token.LBRACE:
					depth++
				case token.RPAREN, token.RBRACK, token.RBRACE:
					depth--
				}
			}
			continue
		}
		if t.t != token.FUNC && t.t != token.TYPE && t.t != token.VAR && t.t != token.CONST {
			if t.t != token.SEMICOLON {
				decoStart, decos = -1, nil
			}
			continue
		}
		d := headerDecl{kind: t.t, declStart: t.off, decorators: decos}
		first := t.off
		if decoStart >= 0 {
			first = decoStart
		}
		decoStart, decos = -1, nil
		d.start = lineStart(first)
		for d.start > 0 {
			prev := lineStart(d.start - 1)
			if !strings.HasPrefix(strings.TrimSpace(string(src[prev:d.start])), "//") {
				break
			}
			d.start = prev
		}
		j := i + 1
		if d.kind == token.FUNC && j < len(toks) && toks[j].t == token.LPAREN {
			var names []string
			dd := 0
			for ; j < len(toks); j++ {
				switch toks[j].t {
				case token.LPAREN, token.LBRACK:
					dd++
				case token.RPAREN, token.RBRACK:
					dd--
				case token.IDENT:
					if dd == 1 {
						names = append(names, toks[j].lit)
					}
				}
				if dd == 0 {
					j++
					break
				}
			}
			if len(names) > 0 {
				d.recvBase = names[len(names)-1]
				if len(names) >= 2 {
					d.recvBase = names[1]
				}
			}
		}
		if j < len(toks) && toks[j].t == token.IDENT {
			d.name = toks[j].lit
		}
		// The declaration ends at the first newline (or ;) at its depth.
		dd, prev := 0, token.ILLEGAL
		k := j
		for ; k < len(toks); k++ {
			switch toks[k].t {
			case token.LPAREN, token.LBRACK:
				dd++
			case token.LBRACE:
				if dd == 0 && d.kind == token.FUNC && d.bodyStart == 0 && prev != token.STRUCT && prev != token.INTERFACE {
					d.bodyStart = toks[k].off
				}
				dd++
			case token.RPAREN, token.RBRACK:
				dd--
			case token.RBRACE:
				dd--
				if dd == 0 && d.bodyStart != 0 && d.bodyEnd == 0 {
					d.bodyEnd = toks[k].off
				}
			}
			if dd == 0 && toks[k].t == token.SEMICOLON {
				break
			}
			prev = toks[k].t
		}
		if k < len(toks) {
			d.end = lineEnd(toks[k].off - 1)
			if toks[k].lit == "\n" {
				d.end = toks[k].off + 1
			}
		} else {
			d.end = to
		}
		d.end = min(d.end, to)
		out = append(out, d)
		i = k
	}
	return out
}

// shapeFile is a .vuka.html file read for a refactor: its shape, imports
// and header declarations.
type shapeFile struct {
	path    string
	src     []byte
	shape   *transpile.Shape
	imports importSection
	markup  int // the offset its markup starts at
	decls   []headerDecl
}

// readShape reads a .vuka.html file's shape; nil when it has none or its
// header doesn't parse.
func readShape(path string, src []byte) *shapeFile {
	sh, err := transpile.ParseShape(src)
	if sh == nil || err != nil || sh.Markup < 0 {
		return nil
	}
	f := &shapeFile{path: path, src: src, shape: sh, imports: parseImports(src)}
	f.markup = lineOffset(src, sh.Markup)
	f.decls = headerDecls(src, f.imports.end, f.markup)
	return f
}

// lineOffset is where the line (from 0) starts.
func lineOffset(src []byte, line int) int {
	off := 0
	for l := 0; l < line && off < len(src); l++ {
		i := bytes.IndexByte(src[off:], '\n')
		if i < 0 {
			return len(src)
		}
		off += i + 1
	}
	return off
}

// methods are the shape's methods in its header.
func (f *shapeFile) methods() []headerDecl {
	var out []headerDecl
	for _, d := range f.decls {
		if d.kind == token.FUNC && d.recvBase == f.shape.Type {
			out = append(out, d)
		}
	}
	return out
}

// stays reports whether a method stays in the header when all are moved,
// and why.
func (f *shapeFile) stays(d headerDecl) (string, bool) {
	switch {
	case len(d.decorators) > 0:
		return d.name + " is decorated (" + d.decorators[0] + ")", true
	case d.name == "Title":
		return "Title names the page", true
	case f.shape.Live && !f.shape.EmbedsLive && strings.HasPrefix(f.shape.LiveWhy, "the markup sends") &&
		token.IsExported(d.name) && transpile.SendsEvent(f.src, d.name):
		return f.shape.Type + " is live because its markup sends " + d.name, true
	}
	return "", false
}

// logicPath is the .vuka file beside a .vuka.html file.
func logicPath(path string) string { return strings.TrimSuffix(path, ".html") }

// moveActions are the move refactors at the request's range in a shape
// file's header: the methods selected, and all that may move.
func moveActions(r actionRequest) []any {
	f := readShape(r.path, r.src)
	if f == nil || r.start >= f.markup {
		return nil
	}
	target := filepath.Base(logicPath(r.path))
	var out []any
	var picked, all []headerDecl
	for _, d := range f.methods() {
		_, stay := f.stays(d)
		if !stay {
			all = append(all, d)
		}
		if r.start < d.end && (r.end > d.start || r.start == r.end && r.start >= d.start) {
			if len(d.decorators) == 0 {
				picked = append(picked, d)
			}
		}
	}
	if len(picked) > 0 {
		title := "Move " + picked[0].name + " to " + target
		if len(picked) > 1 {
			title = "Move " + strconv.Itoa(len(picked)) + " methods to " + target
		}
		if c, err := moveMethods(f, picked, r.read); err == nil {
			out = append(out, codeAction{Title: title, Kind: "refactor.move", Edit: c.workspaceEdit(r.uri)})
		}
	}
	if len(all) > 0 && (len(picked) == 0 || len(all) > len(picked)) {
		if c, err := moveMethods(f, all, r.read); err == nil {
			out = append(out, codeAction{Title: "Move the methods of " + f.shape.Type + " to " + target, Kind: "refactor.move", Edit: c.workspaceEdit(r.uri)})
		}
	}
	return out
}

// moveMethods moves the declarations ds of a shape file's header into its
// logic file.
func moveMethods(f *shapeFile, ds []headerDecl, read load.ReadFunc) (change, error) {
	var cuts []edit
	var moved [][]byte
	for _, d := range ds {
		start := d.start
		if prev := bytes.LastIndexByte(f.src[:max(start-1, 0)], '\n') + 1; start > 0 && strings.TrimSpace(string(f.src[prev:start])) == "" {
			start = prev // the blank line above it goes with it
		}
		cuts = append(cuts, edit{start, d.end, ""})
		moved = append(moved, f.src[d.start:d.end])
	}
	return moveDecls(f.path, f.src, f.imports, cuts, moved, logicPath(f.path), read)
}

// moveDecls is the change moving the text moved, cut from src (the file at
// path) at cuts, to the end of the file to (created when it isn't there),
// with the imports it needs; src loses the imports it no longer uses.
func moveDecls(path string, src []byte, imps importSection, cuts []edit, moved [][]byte, to string, read load.ReadFunc) (change, error) {
	root, modPath, _ := load.ModuleRoot(filepath.Dir(path))
	pkgName := pathPackageNames(root, modPath)
	names := map[string]importSpec{}
	for _, sp := range imps.specs {
		names[sp.localName(pkgName)] = sp
	}
	body := bytes.Join(moved, []byte("\n"))
	var need []wantImport
	for n := range usedNames(body) {
		if sp, ok := names[n]; ok {
			need = append(need, wantImport{sp.name, sp.path})
		}
	}
	sortImports(need)
	rest := applyEdits(src, cuts)
	restCode := rest[parseImports(rest).end:]
	drop := unusedImports(rest, restCode, pkgName)
	srcEdits := append(cuts, importEdits(src, drop, nil)...)

	var c change
	c = append(c, fileChange{path: path, old: src, edits: srcEdits})
	old, err := read(to)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		var b strings.Builder
		b.WriteString("package " + imps.pkg + "\n")
		if len(need) == 1 {
			b.WriteString("\nimport " + importLine(need[0]) + "\n")
		} else if len(need) > 1 {
			b.WriteString("\nimport (\n")
			std := true
			for i, n := range need {
				if i > 0 && std && !isStd(n.path) {
					b.WriteString("\n")
				}
				std = isStd(n.path)
				b.WriteString("\t" + importLine(n) + "\n")
			}
			b.WriteString(")\n")
		}
		b.WriteString("\n")
		b.Write(body)
		c = append(c, created(to, b.String()))
		return c, nil
	}
	tail := "\n"
	if len(old) > 0 && old[len(old)-1] != '\n' {
		tail = "\n\n"
	}
	edits := importEdits(old, nil, need)
	edits = append(edits, edit{len(old), len(old), tail + string(body)})
	c = append(c, fileChange{path: to, old: old, edits: edits})
	return c, nil
}

func importLine(w wantImport) string {
	if w.name != "" {
		return w.name + " " + strconv.Quote(w.path)
	}
	return strconv.Quote(w.path)
}

// isStd reports whether an import path is the standard library's.
func isStd(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

// sortImports orders imports as gofmt's groups do: the standard library's
// first.
func sortImports(list []wantImport) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && importLess(list[j], list[j-1]); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

func importLess(a, b wantImport) bool {
	if isStd(a.path) != isStd(b.path) {
		return isStd(a.path)
	}
	return a.path < b.path
}

// trackedUse is a method of a .go file using a shape's tracked state.
type trackedUse struct {
	decl  headerDecl
	field string
	shape *shapeFile
}

// trackedInGo finds the methods of the .go file src, of the shapes of its
// package's .vuka.html files, that read or write their tracked state
// (E0155): those the compiler can't lower there.
func trackedInGo(path string, src []byte, read load.ReadFunc) []trackedUse {
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
	if af == nil || err != nil && len(af.Decls) == 0 {
		return nil
	}
	shapes := map[string]*shapeFile{}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".vuka.html") {
			continue
		}
		p := filepath.Join(filepath.Dir(path), e.Name())
		s, err := read(p)
		if err != nil {
			continue
		}
		if sf := readShape(p, s); sf != nil {
			shapes[sf.shape.Type] = sf
		}
	}
	if len(shapes) == 0 {
		return nil
	}
	decls := headerDecls(src, parseImports(src).end, len(src))
	var out []trackedUse
	for _, d := range af.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 || fd.Body == nil {
			continue
		}
		_, base := recvOf(fd.Recv.List[0].Type)
		sf := shapes[base]
		if sf == nil || len(fd.Recv.List[0].Names) != 1 {
			continue
		}
		recv := fd.Recv.List[0].Names[0].Name
		tracked := sf.trackedFields()
		field := ""
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if s, ok := n.(*ast.SelectorExpr); ok && field == "" {
				if x, ok := s.X.(*ast.Ident); ok && x.Name == recv && tracked[s.Sel.Name] {
					field = s.Sel.Name
				}
			}
			return field == ""
		})
		if field == "" {
			continue
		}
		start := fset.Position(fd.Pos()).Offset
		for _, hd := range decls {
			if hd.declStart == start || hd.start <= start && start < hd.end {
				out = append(out, trackedUse{decl: hd, field: field, shape: sf})
				break
			}
		}
	}
	return out
}

// trackedFields are the shape's fields that are tracked server state, as
// far as their written types tell: unexported, not a state, and not a
// dependency (a pointer, func, chan or interface) or a sync value.
func (f *shapeFile) trackedFields() map[string]bool {
	out := map[string]bool{}
	for _, fl := range f.shape.Fields {
		t := strings.TrimSpace(fl.Type)
		if fl.Role != transpile.ServerState || strings.HasPrefix(t, "*") || strings.HasPrefix(t, "func") ||
			strings.HasPrefix(t, "chan") || strings.HasPrefix(t, "<-chan") || strings.HasPrefix(t, "interface") ||
			strings.HasPrefix(t, "sync.") || strings.HasPrefix(t, "atomic.") || t == "error" || t == "any" {
			continue
		}
		out[fl.Name] = true
	}
	return out
}

func recvOf(x ast.Expr) (ptr bool, name string) {
	if s, ok := x.(*ast.StarExpr); ok {
		ptr, x = true, s.X
	}
	switch t := x.(type) {
	case *ast.IndexExpr:
		x = t.X
	case *ast.IndexListExpr:
		x = t.X
	}
	if id, ok := x.(*ast.Ident); ok {
		return ptr, id.Name
	}
	return ptr, ""
}

// goFileFixes are the fixes for a .go file: a method using a shape's
// tracked state moved to the shape's logic file (E0155).
func goFileFixes(r actionRequest) []any {
	var out []any
	for _, u := range trackedInGo(r.path, r.src, r.read) {
		d := u.decl
		diag := r.diag("E0155")
		if !(r.start < d.end && (r.end > d.start || r.start == r.end && r.start >= d.start)) && diag == nil {
			continue
		}
		if diag != nil {
			if rg, ok := toRange(diag["range"]); ok {
				if off := offsetOf(r.src, rg.Start); off < d.start || off >= d.end {
					continue
				}
			}
		}
		to := logicPath(u.shape.path)
		if strings.HasSuffix(r.path, "_test.go") {
			to = strings.TrimSuffix(to, ".vuka") + "_test.vuka"
		}
		c, err := moveGoDecl(r.path, r.src, d, to, r.read)
		if err != nil {
			continue
		}
		a := codeAction{Title: "Move " + d.name + " to " + filepath.Base(to), Kind: "quickfix", IsPreferred: true, Edit: c.workspaceEdit(r.uri)}
		if diag != nil {
			a.Diagnostics = []map[string]any{diag}
		}
		out = append(out, a)
	}
	return out
}

// moveGoDecl moves a declaration of a .go file to the end of a .vuka file;
// a .go file left with nothing but its package clause and imports goes.
func moveGoDecl(path string, src []byte, d headerDecl, to string, read load.ReadFunc) (change, error) {
	start := d.start
	if prev := bytes.LastIndexByte(src[:max(start-1, 0)], '\n') + 1; start > 0 && strings.TrimSpace(string(src[prev:start])) == "" {
		start = prev
	}
	imps := parseImports(src)
	c, err := moveDecls(path, src, imps, []edit{{start, d.end, ""}}, [][]byte{src[d.start:d.end]}, to, read)
	if err != nil {
		return nil, err
	}
	rest := applyEdits(src, []edit{{start, d.end, ""}})
	if strings.TrimSpace(string(rest[parseImports(rest).end:])) == "" && !bytes.Contains(src[:imps.pkgEnd], []byte("//go:build")) {
		c[0] = fileChange{path: path, remove: true}
	}
	return c, nil
}
