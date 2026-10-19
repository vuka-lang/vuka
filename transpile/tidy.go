package transpile

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// tidyFile is one .vuka file's raw output on its way to readable Go.
type tidyFile struct {
	f    *fileState
	src  []byte
	m    *SourceMap
	fset *token.FileSet
	ast  *ast.File
}

// tidy turns the package's generated files into the Go a person would
// write: gofmt'd, real names for what Vuka declares (no __ prefixes), doc
// comments where they belong, one normal import block, and a //line
// directive (naming the file as Options.Path does) only where the code stops
// following the .vuka file line by line. A file it can't tidy stays as it is.
func (e *engine) tidy(srcs [][]byte, srcMaps []*SourceMap) ([][]byte, []*SourceMap) {
	files := make([]*tidyFile, len(srcs))
	for i, src := range srcs {
		t := &tidyFile{f: e.vuka[i], src: src, m: srcMaps[i], fset: token.NewFileSet()}
		if a, err := parser.ParseFile(t.fset, "", src, parser.ParseComments|parser.SkipObjectResolution); err == nil {
			t.ast = a
		}
		files[i] = t
	}
	renames := e.renames(files)
	outs, outMaps := make([][]byte, len(srcs)), make([]*SourceMap, len(srcs))
	for i, t := range files {
		outs[i], outMaps[i] = t.src, t.m
		if t.ast == nil {
			continue
		}
		if b, segs, ok := t.tidy(renames); ok {
			outs[i], outMaps[i] = b, &SourceMap{segs: segs, spellings: t.m.spellings}
		}
	}
	return outs, outMaps
}

// renames gives every __-prefixed name Vuka declared in the package's files
// a plain one, unused anywhere in the package, so no rename captures or
// shadows another name.
func (e *engine) renames(files []*tidyFile) map[string]string {
	taken := map[string]bool{"init": true, "main": true, "_": true}
	for _, n := range types.Universe.Names() {
		taken[n] = true
	}
	scan := func(src []byte) {
		var s scanner.Scanner
		fs := token.NewFileSet()
		s.Init(fs.AddFile("", -1, len(src)), src, nil, 0)
		for {
			_, tok, lit := s.Scan()
			if tok == token.EOF {
				break
			}
			if tok == token.IDENT {
				taken[lit] = true
			}
		}
	}
	for _, f := range e.files {
		if !f.vuka {
			scan(f.src)
		}
	}
	inGo := maps.Clone(taken) // a name a .go file uses keeps its spelling
	var old []string
	seen := map[string]bool{}
	funcs := map[string]bool{}
	pkgs := map[string]string{} // import name → the package's own
	for _, t := range files {
		scan(t.src)
		if t.ast == nil {
			continue
		}
		ast.Inspect(t.ast, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.Ident:
				if strings.HasPrefix(n.Name, "__") && !seen[n.Name] && !inGo[n.Name] {
					seen[n.Name] = true
					old = append(old, n.Name)
				}
			case *ast.FuncDecl:
				funcs[n.Name.Name] = true
			case *ast.ImportSpec:
				if n.Name != nil {
					path, _ := strconv.Unquote(n.Path.Value)
					pkgs[n.Name.Name] = pathBase(path)
				}
			}
			return true
		})
	}
	sort.Strings(old)
	out := map[string]string{}
	for _, name := range old {
		base := plainName(name)
		if p := pkgs[name]; token.IsIdentifier(p) && !taken[p] {
			base = p
		}
		cand := base
		if funcs[name] && taken[cand] {
			base += "Impl"
			cand = base
		}
		for i := 2; taken[cand] || token.IsKeyword(cand); i++ {
			cand = base + strconv.Itoa(i)
		}
		taken[cand] = true
		out[name] = cand
	}
	return out
}

// plainName is a generated name without its underscores, in camel case and
// unexported: __Board_type is boardType, __m1 m1.
func plainName(name string) string {
	var b strings.Builder
	for i, part := range strings.FieldsFunc(name, func(r rune) bool { return r == '_' }) {
		r, n := utf8.DecodeRuneInString(part)
		if i == 0 {
			r = unicode.ToLower(r)
		} else {
			r = unicode.ToUpper(r)
		}
		b.WriteRune(r)
		b.WriteString(part[n:])
	}
	if b.Len() == 0 {
		return "v"
	}
	return b.String()
}

// node is a node of a generated file, in the order walk finds them.
type node struct {
	pos, end token.Pos
	anchor   bool // a //line directive may go before it: a declaration, a statement, a case, a field, a literal's element
	fn       bool // a top-level function other than init
	doc      bool // a top-level declaration, or the package clause, with a doc comment
	leaf     bool // an identifier or a literal
}

// walk lists file's nodes but its comments, imports and parentheses, the
// parts formatting may change, so a file and its formatted self list the
// same nodes in the same order.
func walk(file *ast.File) []node {
	out := []node{{pos: file.Package, anchor: true, doc: file.Doc != nil}}
	anchors, top := map[ast.Node]bool{}, map[ast.Node]bool{}
	for _, d := range file.Decls {
		anchors[d], top[d] = true, true
	}
	mark := func(ns ...ast.Node) {
		for _, n := range ns {
			anchors[n] = true
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case nil, *ast.File:
			return true
		case *ast.CommentGroup, *ast.Comment:
			return false
		case *ast.ParenExpr:
			return true
		case *ast.GenDecl:
			if n.Tok == token.IMPORT {
				return false
			}
			if n.Lparen.IsValid() {
				for _, s := range n.Specs {
					mark(s)
				}
			}
		case *ast.BlockStmt:
			for _, s := range n.List {
				mark(s)
			}
		case *ast.CaseClause:
			for _, s := range n.Body {
				mark(s)
			}
		case *ast.CommClause:
			for _, s := range n.Body {
				mark(s)
			}
		case *ast.StructType:
			for _, f := range n.Fields.List {
				mark(f)
			}
		case *ast.InterfaceType:
			for _, f := range n.Methods.List {
				mark(f)
			}
		case *ast.CompositeLit:
			for _, x := range n.Elts {
				mark(x)
			}
		}
		nd := node{pos: n.Pos(), end: n.End(), anchor: anchors[n]}
		switch n := n.(type) {
		case *ast.FuncDecl:
			nd.fn, nd.doc = n.Name.Name != "init", n.Doc != nil
		case *ast.GenDecl:
			nd.doc = n.Doc != nil && top[n]
		case *ast.Ident, *ast.BasicLit:
			nd.leaf = true
		}
		out = append(out, nd)
		return true
	})
	return out
}

func (t *tidyFile) tidy(renames map[string]string) ([]byte, []segment, bool) {
	file, tf := t.ast, t.fset.File(t.ast.Pos())
	srcLines := t.f.lines
	srcBlank := func(off int) bool {
		p := srcLines.pos("", off)
		end := len(t.f.src)
		if p.Line < len(srcLines) {
			end = srcLines[p.Line]
		}
		return len(bytes.TrimSpace(t.f.src[srcLines[p.Line-1]:end])) == 0
	}

	ast.Inspect(file, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if r, ok := renames[id.Name]; ok {
				id.Name = r
			}
		}
		return true
	})

	// The comments Vuka leaves: line directives, and the empty ones
	// standing for attributes and statics.
	var stripped [][2]int
	var groups []*ast.CommentGroup
	dropped := map[*ast.CommentGroup]bool{}
	for _, g := range file.Comments {
		var keep []*ast.Comment
		for _, c := range g.List {
			if t.generatedComment(tf, c) {
				stripped = append(stripped, [2]int{tf.Offset(c.Pos()), tf.Offset(c.End())})
				continue
			}
			keep = append(keep, c)
		}
		if len(keep) == 0 {
			dropped[g] = true
			continue
		}
		g.List = keep
		groups = append(groups, g)
	}
	file.Comments = groups
	clearDropped(file, dropped)

	// Imports Vuka adds go on the package clause's line, to keep the
	// source's lines; they join the file's import block.
	pkgLine := tf.Line(file.Package)
	var added []*ast.ImportSpec
	decls := file.Decls[:0]
	for _, d := range file.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT && tf.Line(g.TokPos) == pkgLine {
			for _, s := range g.Specs {
				added = append(added, s.(*ast.ImportSpec))
			}
			continue
		}
		decls = append(decls, d)
	}
	file.Decls = decls
	var imports []*ast.ImportSpec
	for _, s := range file.Imports {
		if !slices.Contains(added, s) {
			imports = append(imports, s)
		}
	}
	file.Imports = imports

	// Lines left empty by what Vuka removed or blanked close up, as if
	// they had never been there.
	lines := []int{0}
	k := 0 // the first stripped comment not ending before the line
	for l := 2; l <= tf.LineCount(); l++ {
		start := tf.Offset(tf.LineStart(l))
		end := len(t.src)
		if l < tf.LineCount() {
			end = tf.Offset(tf.LineStart(l + 1))
		}
		for k < len(stripped) && stripped[k][1] <= start {
			k++
		}
		blank, cut := true, false
		for i, j := start, k; i < end && blank; i++ {
			if j < len(stripped) && i >= stripped[j][0] {
				i, cut = stripped[j][1]-1, true
				j++
				continue
			}
			blank = t.src[i] == ' ' || t.src[i] == '\t' || t.src[i] == '\n' || t.src[i] == '\r'
		}
		if blank && !cut {
			off, exact, ok := t.m.ToSource(start)
			cut = ok && exact && !srcBlank(off)
		}
		if !blank || !cut {
			lines = append(lines, start)
		}
	}
	if !tf.SetLines(lines) {
		return nil, nil, false
	}

	type mapping struct {
		src, line int // in the .vuka file; line 0 for none
		exact     bool
	}
	before := walk(file)
	mapped := make([]mapping, len(before))
	for i, n := range before {
		if off, exact, ok := t.m.ToSource(tf.Offset(n.pos)); ok {
			mapped[i] = mapping{off, srcLines.pos("", off).Line, exact}
		}
	}

	var buf bytes.Buffer
	if err := format.Node(&buf, t.fset, file); err != nil {
		return nil, nil, false
	}
	text := buf.Bytes()
	if len(added) > 0 {
		var err error
		if text, err = addImports(text, added); err != nil {
			return nil, nil, false
		}
	}

	fset := token.NewFileSet()
	out, err := parser.ParseFile(fset, "", text, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, nil, false
	}
	otf := fset.File(out.Pos())
	after := walk(out)
	if len(after) != len(before) {
		return nil, nil, false
	}
	// A line follows the source line of its first copied code; one of
	// only generated code, that of what generated it when it starts a
	// function (for stack traces), else whatever the lines before give it.
	type lineInfo struct {
		col, src int
		anchor   bool
		fnSrc    int
	}
	info := map[int]*lineInfo{}
	for i, n := range after {
		p := otf.Position(n.pos)
		li := info[p.Line]
		if li == nil {
			li = &lineInfo{}
			info[p.Line] = li
		}
		if mapped[i].exact && (li.src == 0 || p.Column < li.col) {
			li.col, li.src = p.Column, mapped[i].line
		}
		if !n.anchor {
			continue
		}
		off := otf.Offset(n.pos)
		if len(bytes.TrimSpace(text[bytes.LastIndexByte(text[:off], '\n')+1:off])) == 0 {
			li.anchor = true
			if n.fn && li.fnSrc == 0 {
				li.fnSrc = mapped[i].line
			}
		}
	}
	var outLines []int
	for l, li := range info {
		if li.anchor {
			outLines = append(outLines, l)
		}
	}
	sort.Ints(outLines)
	directives := map[int]int{} // output line → .vuka line
	dirOut, dirSrc := 0, 0
	for _, l := range outLines {
		src := info[l].src
		if src == 0 {
			src = info[l].fnSrc
		}
		if src == 0 || dirOut > 0 && dirSrc+l-dirOut == src {
			continue
		}
		directives[l] = src
		dirOut, dirSrc = l, src
	}
	docs := map[int]bool{} // output lines of top-level declarations with a doc comment
	for _, n := range after {
		if n.doc {
			docs[otf.Line(n.pos)] = true
		}
	}

	// The directives go in, after a doc comment's text the empty line gofmt
	// puts before directives; then the map follows each node's first token
	// to the .vuka file.
	var b bytes.Buffer
	textLines := bytes.SplitAfter(text, []byte("\n"))
	for i, line := range textLines {
		if l, ok := directives[i+1]; ok {
			if docs[i+1] && i > 0 && !isDirective(bytes.TrimSpace(textLines[i-1])) {
				b.WriteString("//\n")
			}
			b.WriteString("//line " + t.f.path + ":" + strconv.Itoa(l) + "\n")
		}
		b.Write(line)
	}
	final, err := format.Source(b.Bytes())
	if err != nil {
		return nil, nil, false
	}
	fset = token.NewFileSet()
	if out, err = parser.ParseFile(fset, "", final, parser.SkipObjectResolution); err != nil {
		return nil, nil, false
	}
	ftf := fset.File(out.Pos())
	if after = walk(out); len(after) != len(before) {
		return nil, nil, false
	}
	var segs []segment
	seen := map[int]bool{}
	for i, n := range after {
		gen := ftf.Offset(n.pos)
		if mapped[i].line == 0 || seen[gen] {
			continue
		}
		seen[gen] = true
		size := ftf.Offset(n.end) - gen
		if !n.leaf {
			size = tokenLen(final[gen:])
		}
		if mapped[i].exact {
			segs = append(segs, segment{gen: gen, src: mapped[i].src, genLen: size, srcLen: size, copy: true})
		} else {
			segs = append(segs, segment{gen: gen, src: mapped[i].src, genLen: size})
		}
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].gen < segs[j].gen })
	return final, segs, true
}

// tokenLen is the length of the keyword, name or operator text starts with.
func tokenLen(text []byte) int {
	n := 0
	for n < len(text) {
		r, size := utf8.DecodeRune(text[n:])
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		n += size
	}
	return max(n, 1)
}

// isDirective reports whether a line of a doc comment is a directive or the
// empty line before them.
func isDirective(line []byte) bool {
	c := string(line)
	if c == "//" || strings.HasPrefix(c, "//line ") || strings.HasPrefix(c, "//extern ") || strings.HasPrefix(c, "//export ") {
		return true
	}
	name, _, ok := strings.Cut(strings.TrimPrefix(c, "//"), ":")
	return ok && strings.HasPrefix(c, "//") && name != "" && strings.TrimLeft(name, "abcdefghijklmnopqrstuvwxyz0123456789") == ""
}

// generatedComment reports whether c is a comment Vuka wrote that says
// nothing: a line directive, or an empty comment standing in for an
// attribute or a static.
func (t *tidyFile) generatedComment(tf *token.File, c *ast.Comment) bool {
	if strings.HasPrefix(c.Text, "//line ") || strings.HasPrefix(c.Text, "/*line ") {
		return true
	}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(c.Text, "//"), "/*"), "*/"))
	if body != "" {
		return false
	}
	_, exact, ok := t.m.ToSource(tf.Offset(c.Pos()))
	return !ok || !exact
}

// clearDropped unhooks the comment groups tidy removed from the nodes
// they were documenting.
func clearDropped(file *ast.File, dropped map[*ast.CommentGroup]bool) {
	if len(dropped) == 0 {
		return
	}
	unhook := func(g **ast.CommentGroup) {
		if *g != nil && dropped[*g] {
			*g = nil
		}
	}
	unhook(&file.Doc)
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			unhook(&n.Doc)
		case *ast.GenDecl:
			unhook(&n.Doc)
		case *ast.Field:
			unhook(&n.Doc)
			unhook(&n.Comment)
		case *ast.ValueSpec:
			unhook(&n.Doc)
			unhook(&n.Comment)
		case *ast.TypeSpec:
			unhook(&n.Doc)
			unhook(&n.Comment)
		case *ast.ImportSpec:
			unhook(&n.Doc)
			unhook(&n.Comment)
		}
		return true
	})
}

// addImports puts specs in the first import block of a formatted file, as a
// group of their own, and formats it again.
func addImports(text []byte, specs []*ast.ImportSpec) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", text, parser.ImportsOnly)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, s := range file.Imports {
		have[importText(s)] = true
	}
	var lines []string
	for _, s := range specs {
		if l := importText(s); !have[l] {
			have[l] = true
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return text, nil
	}
	tf := fset.File(file.Pos())
	var out []byte
	switch {
	case len(file.Imports) == 0:
		at := tf.Offset(file.Name.End())
		out = append(out, text[:at]...)
		out = append(out, "\n\nimport (\n\t"+strings.Join(lines, "\n\t")+"\n)"...)
		out = append(out, text[at:]...)
	default:
		g := file.Decls[0].(*ast.GenDecl)
		var body []string
		if !g.Lparen.IsValid() {
			body = append(body, string(text[tf.Offset(g.Specs[0].Pos()):tf.Offset(g.End())]))
		}
		group := "\n\n\t"
		if last := file.Imports[len(file.Imports)-1]; strings.Contains(strings.SplitN(last.Path.Value, "/", 2)[0], ".") {
			group = "\n\t"
		}
		if g.Lparen.IsValid() {
			at := tf.Offset(g.Rparen)
			out = append(out, bytes.TrimRight(text[:at], "\n\t ")...)
			out = append(out, group+strings.Join(lines, "\n\t")+"\n"...)
			out = append(out, text[at:]...)
		} else {
			out = append(out, text[:tf.Offset(g.TokPos)]...)
			out = append(out, "import ("+"\n\t"+body[0]+group+strings.Join(lines, "\n\t")+"\n)"...)
			out = append(out, text[tf.Offset(g.End()):]...)
		}
	}
	return out, nil
}

// importText is an import spec as a line of an import block: the name only
// when it isn't the package's own.
func importText(s *ast.ImportSpec) string {
	path, _ := strconv.Unquote(s.Path.Value)
	if s.Name == nil || s.Name.Name == pathBase(path) {
		return strconv.Quote(path)
	}
	return s.Name.Name + " " + strconv.Quote(path)
}

func pathBase(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}
