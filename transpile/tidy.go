package transpile

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/scanner"
	"go/token"
	"go/types"
	"maps"
	"regexp"
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
			outs[i], outMaps[i] = b, &SourceMap{segs: segs, spellings: renamedSpellings(t.m.spellings, renames)}
		}
	}
	return outs, outMaps
}

// renamedSpellings are spellings quoting the code as tidied: its
// __-prefixed names renamed.
func renamedSpellings(sps []spelling, renames map[string]string) []spelling {
	out := make([]spelling, len(sps))
	for i, sp := range sps {
		sp.gen = tidyName.ReplaceAllStringFunc(sp.gen, func(n string) string {
			if r, ok := renames[n]; ok {
				return r
			}
			return n
		})
		out[i] = sp
	}
	return out
}

var tidyName = regexp.MustCompile(`\b__\w+`)

// renames gives every __-prefixed name Vuka declared in the package's files
// a plain one, unused anywhere in the package, so no rename captures or
// shadows another name.
func (e *engine) renames(files []*tidyFile) map[string]string {
	taken := map[string]bool{"init": true, "main": true, "_": true}
	for _, n := range types.Universe.Names() {
		taken[n] = true
	}
	// idents and imports are each file's identifiers and the names it
	// imports packages under (by path): an auto-import may take the
	// package's own name where every file using that name imports the
	// package under it.
	var idents []map[string]bool
	var imports []map[string]string
	scan := func(src []byte) {
		ids := map[string]bool{}
		var s scanner.Scanner
		fs := token.NewFileSet()
		s.Init(fs.AddFile("", -1, len(src)), src, nil, 0)
		for {
			_, tok, lit := s.Scan()
			if tok == token.EOF {
				break
			}
			if tok == token.IDENT {
				taken[lit], ids[lit] = true, true
			}
		}
		idents = append(idents, ids)
		imp := map[string]string{}
		if a, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ImportsOnly); err == nil {
			for _, is := range a.Imports {
				path, _ := strconv.Unquote(is.Path.Value)
				name := ImportName(path)
				if is.Name != nil {
					name = is.Name.Name
				}
				imp[name] = path
			}
		}
		imports = append(imports, imp)
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
				taken[n.Name] = true
				if strings.HasPrefix(n.Name, "__") && !seen[n.Name] && !inGo[n.Name] {
					seen[n.Name] = true
					old = append(old, n.Name)
				}
			case *ast.FuncDecl:
				funcs[n.Name.Name] = true
			case *ast.ImportSpec:
				if n.Name != nil {
					path, _ := strconv.Unquote(n.Path.Value)
					pkgs[n.Name.Name] = ImportName(path)
				}
			}
			return true
		})
	}
	sort.Strings(old)
	out := e.localNames(byOrdinal(old))
	for _, n := range out {
		taken[n] = true
	}
	for _, name := range old {
		if out[name] != "" {
			continue
		}
		base := plainName(name)
		if p := pkgs[name]; token.IsIdentifier(p) && taken[p] && ownName(name, p, idents, imports) {
			out[name] = p
			continue
		} else if token.IsIdentifier(p) && !taken[p] {
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

// localName is the name Go code gives a local a lowering declares: the
// error and ok a ? checks (__e6, __ok5) err and ok, the value it returns
// (__v7_1) v.
var localName = []struct {
	re   *regexp.Regexp
	name string
}{{regexp.MustCompile(`^__e\d+$`), "err"}, {regexp.MustCompile(`^__ok\d+$`), "ok"}, {regexp.MustCompile(`^__v\d+_1$`), "v"}}

// byOrdinal is names with the variables a lowering named after what they
// hold (__h3_user) in the order it declared them, so the first takes the
// name and a later one the fallback.
func byOrdinal(names []string) []string {
	out := slices.Clone(names)
	n := func(s string) int {
		if m := hintName.FindStringSubmatch(s); m != nil {
			v, _ := strconv.Atoi(strings.TrimPrefix(strings.SplitN(s, "_", 4)[2], "h"))
			return v
		}
		return 0
	}
	sort.SliceStable(out, func(i, j int) bool { return n(out[i]) < n(out[j]) })
	return out
}

// hintName is a lowering's variable named after what it holds
// (tmpName): __h3_user is user, else v, v2, ….
var hintName = regexp.MustCompile(`^__h\d+_(\w+)$`)

// localNames renames the locals of old a lowering declares as localName
// says, wherever go/types shows the new name captures and shadows nothing:
// no other declaration of it in the local's scope, no use of the local
// meaning another variable by it, no use of another variable by it the
// local would take.
func (e *engine) localNames(old []string) map[string]string {
	out := map[string]string{}
	if e.info == nil || e.pkg == nil {
		return out
	}
	vars := map[string][]*types.Var{}
	for id, obj := range e.info.Defs {
		if v, ok := obj.(*types.Var); ok && v.Parent() != nil && v.Parent() != e.pkg.Scope() && strings.HasPrefix(id.Name, "__") {
			vars[id.Name] = append(vars[id.Name], v)
		}
	}
	uses := map[types.Object][]token.Pos{}
	byName := map[string][]*ast.Ident{}
	for id, obj := range e.info.Uses {
		uses[obj] = append(uses[obj], id.Pos())
		byName[id.Name] = append(byName[id.Name], id)
	}
	renamed := map[string][]*types.Var{} // the locals given each name so far
	encloses := func(outer, s *types.Scope) bool {
		for ; s != nil; s = s.Parent() {
			if s == outer {
				return true
			}
		}
		return false
	}
	// at is the scope n resolves to at p, counting the renamed locals.
	at := func(p token.Pos, n string) *types.Scope {
		inner := e.pkg.Scope().Innermost(p)
		if inner == nil {
			return nil
		}
		found, _ := inner.LookupParent(n, p)
		for s := inner; s != nil && s != found; s = s.Parent() {
			for _, v := range renamed[n] {
				if v.Parent() == s && v.Pos() < p {
					return s
				}
			}
		}
		return found
	}
	// visible reports whether v is in scope at p: past the end of the
	// statement declaring it, so its name may also name what that
	// statement calls (exists, err := exists(ctx)).
	visible := func(v *types.Var, p token.Pos) bool {
		inner := e.pkg.Scope().Innermost(p)
		if inner == nil {
			return p > v.Pos()
		}
		_, obj := inner.LookupParent(v.Name(), p)
		return obj == v
	}
	safe := func(v *types.Var, n string) bool {
		if token.IsKeyword(n) {
			return false
		}
		s := v.Parent()
		if s.Lookup(n) != nil || slices.ContainsFunc(renamed[n], func(w *types.Var) bool { return w.Parent() == s }) {
			return false
		}
		for _, p := range uses[v] {
			if found := at(p, n); found != nil && !encloses(found, s) {
				return false
			}
		}
		for _, id := range byName[n] {
			if obj := e.info.Uses[id]; obj != nil && obj.Parent() != nil && s.Contains(id.Pos()) && visible(v, id.Pos()) && encloses(obj.Parent(), s) {
				return false
			}
		}
		for _, w := range renamed[n] {
			for _, p := range uses[w] {
				if s.Contains(p) && p > v.Pos() && encloses(w.Parent(), s) {
					return false
				}
			}
		}
		return true
	}
	for _, name := range old {
		var cands []string
		for _, l := range localName {
			if l.re.MatchString(name) {
				cands = []string{l.name}
			}
		}
		if m := hintName.FindStringSubmatch(name); m != nil {
			cands = []string{m[1], "v"}
			for i := 2; i < 10; i++ {
				cands = append(cands, "v"+itoa(i))
			}
		}
		vs := vars[name]
		if len(vs) == 0 {
			continue
		}
		for _, n := range cands {
			ok := true
			for _, v := range vs {
				ok = ok && safe(v, n)
			}
			if ok {
				renamed[n] = append(renamed[n], vs...)
				out[name] = n
				break
			}
		}
	}
	return out
}

// ownName reports whether the auto-import name may be p, its package's
// own name: no file using it uses p, and every file using p imports the
// same package as p.
func ownName(name, p string, idents []map[string]bool, imports []map[string]string) bool {
	path := ""
	for i, ids := range idents {
		switch {
		case ids[name] && ids[p]:
			return false
		case ids[name]:
			path = imports[i][name]
		}
	}
	for i, ids := range idents {
		if ids[p] && (imports[i][p] == "" || imports[i][p] != path) {
			return false
		}
	}
	return path != ""
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
	pos, end token.Pos // end only for a leaf
	anchor   bool      // a //line directive may go before it: a declaration, a statement, a case, a field, a literal's element
	stmt     bool      // a statement of a block or a case, or a case: a directive before it changes no alignment
	fn       bool      // a top-level function other than init: its line is what generated it
	doc      bool      // a top-level declaration, or the package clause, with a doc comment
	leaf     bool      // an identifier or a literal
}

const (
	markAnchor = 1 << iota
	markTop
	markStmt
)

// walk lists file's nodes but its comments, imports and parentheses, the
// parts formatting may change, so a file and its formatted self list the
// same nodes in the same order.
func walk(file *ast.File) []node {
	out := make([]node, 1, 1024)
	out[0] = node{pos: file.Package, anchor: true, doc: file.Doc != nil}
	marks := make(map[ast.Node]uint8, 256)
	for _, d := range file.Decls {
		marks[d] = markAnchor | markTop
	}
	mark := func(m uint8, ns ...ast.Node) {
		for _, n := range ns {
			marks[n] |= m
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
					mark(markAnchor, s)
				}
			}
		case *ast.BlockStmt:
			for _, s := range n.List {
				mark(markAnchor|markStmt, s)
			}
		case *ast.CaseClause:
			for _, s := range n.Body {
				mark(markAnchor|markStmt, s)
			}
		case *ast.CommClause:
			for _, s := range n.Body {
				mark(markAnchor|markStmt, s)
			}
		case *ast.StructType:
			for _, f := range n.Fields.List {
				mark(markAnchor, f)
			}
		case *ast.InterfaceType:
			for _, f := range n.Methods.List {
				mark(markAnchor, f)
			}
		case *ast.CompositeLit:
			for _, x := range n.Elts {
				mark(markAnchor, x)
			}
		}
		m := marks[n]
		nd := node{pos: n.Pos(), anchor: m&markAnchor != 0, stmt: m&markStmt != 0}
		switch n := n.(type) {
		case *ast.FuncDecl:
			nd.fn, nd.doc = n.Name.Name != "init", n.Doc != nil
		case *ast.GenDecl:
			nd.doc = n.Doc != nil && m&markTop != 0
		case *ast.Ident, *ast.BasicLit:
			nd.leaf, nd.end = true, n.End()
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
		switch n := n.(type) {
		case *ast.FuncDecl:
			// a function Vuka renamed (a decorated one's body, createPostImpl)
			// is named so in its doc comment too
			if r, ok := renames[n.Name.Name]; ok && n.Doc != nil {
				c := n.Doc.List[0]
				if old := "// " + strings.TrimLeft(n.Name.Name, "_"); strings.HasPrefix(c.Text, old) && !continuesIdent([]byte(c.Text[len(old):])) {
					c.Text = "// " + r + c.Text[len(old):]
				}
			}
		case *ast.Ident:
			if r, ok := renames[n.Name]; ok {
				n.Name = r
			}
		case *ast.BasicLit:
			if n.Kind == token.STRING && strings.Contains(n.Value, "{__") {
				n.Value = renameLabels(n.Value, renames)
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
			line := srcLines.pos("", off).Line
			if t.f.html != nil {
				line = t.f.html.lines.pos("", t.f.html.toSrc(off)).Line
			}
			mapped[i] = mapping{off, line, exact}
		}
	}

	var buf bytes.Buffer
	ast.SortImports(t.fset, file)
	if err := gofmt.Fprint(&buf, t.fset, file); err != nil {
		return nil, nil, false
	}
	text, reformat := buf.Bytes(), false
	if len(added) > 0 {
		var err error
		if text, reformat, err = addImports(text, added); err != nil {
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
		stmt     bool // its anchor is a statement or a case
		doc      bool // it starts a top-level declaration with a doc comment
		fnSrc    int
	}
	info := make([]lineInfo, otf.LineCount()+2) // by output line
	for i, n := range after {
		off := otf.Offset(n.pos)
		l := otf.PositionFor(n.pos, false).Line
		li := &info[l]
		col := off - otf.Offset(otf.LineStart(l)) + 1
		if mapped[i].exact && (li.src == 0 || col < li.col) {
			li.col, li.src = col, mapped[i].line
		}
		li.doc = li.doc || n.doc
		if !n.anchor {
			continue
		}
		if len(bytes.TrimSpace(text[off-col+1:off])) == 0 {
			li.anchor, li.stmt = true, n.stmt
			if n.fn && li.fnSrc == 0 {
				li.fnSrc = mapped[i].line
			}
		}
	}
	directives := make([]int, len(info)) // output line → .vuka line, 0 for none
	dirOut, dirSrc := 0, 0
	for l := range info {
		li := &info[l]
		if !li.anchor {
			continue
		}
		src := li.src
		if src == 0 {
			src = li.fnSrc
		}
		if src == 0 || dirOut > 0 && dirSrc+l-dirOut == src {
			continue
		}
		directives[l] = src
		dirOut, dirSrc = l, src
	}

	// The directives go in, after a doc comment's text the empty line gofmt
	// puts before directives; then the map follows each node's first token
	// to the .vuka file. A directive gofmt leaves where it is (at the start
	// of a block or after an empty line or a doc comment) keeps the text
	// gofmt'd and the nodes where they were, but for the lines it adds; any
	// other means formatting again.
	var b bytes.Buffer
	b.Grow(len(text) + 1024)
	shift := make([]int, len(info)+1) // bytes added before each line (1-based)
	settled := !reformat
	prev := []byte(nil) // the line before, trimmed
	for i, start := 0, 0; start < len(text); i++ {
		end := bytes.IndexByte(text[start:], '\n') + 1
		if end == 0 {
			end = len(text) - start
		}
		line := text[start : start+end]
		if i+1 < len(info) && directives[i+1] > 0 {
			switch {
			case info[i+1].doc:
				if i > 0 && !isDirective(prev) {
					b.WriteString("//\n")
				}
			case len(prev) == 0, bytes.HasSuffix(prev, []byte("{")), bytes.HasSuffix(prev, []byte("(")), isDirective(prev):
			case info[i+1].stmt && !hasComment(prev) && !hasComment(line):
				// No trailing comments to align across it.
			default:
				settled = false
			}
			b.WriteString("//line " + t.f.path + ":" + strconv.Itoa(directives[i+1]) + "\n")
		}
		if i+1 < len(shift) {
			shift[i+1] = b.Len() - start
		}
		b.Write(line)
		prev = bytes.TrimSpace(line)
		start += end
	}
	final, ftf := b.Bytes(), otf
	if !settled {
		var err error
		if final, err = format.Source(final); err != nil {
			return nil, nil, false
		}
		fset = token.NewFileSet()
		if out, err = parser.ParseFile(fset, "", final, parser.SkipObjectResolution); err != nil {
			return nil, nil, false
		}
		ftf = fset.File(out.Pos())
		if after = walk(out); len(after) != len(before) {
			return nil, nil, false
		}
		shift = make([]int, ftf.LineCount()+2)
	}
	segs := make([]segment, 0, len(after))
	last := -1
	for i, n := range after {
		gen := ftf.Offset(n.pos) + shift[ftf.PositionFor(n.pos, false).Line]
		if mapped[i].line == 0 || gen == last {
			continue
		}
		last = gen
		size := tokenLen(final[gen:])
		if n.leaf {
			size = ftf.Offset(n.end) + shift[ftf.PositionFor(n.end, false).Line] - gen
		}
		if mapped[i].exact {
			segs = append(segs, segment{gen: gen, src: mapped[i].src, genLen: size, srcLen: size, copy: true})
		} else {
			segs = append(segs, segment{gen: gen, src: mapped[i].src, genLen: size})
		}
	}
	if !sort.SliceIsSorted(segs, func(i, j int) bool { return segs[i].gen < segs[j].gen }) {
		sort.SliceStable(segs, func(i, j int) bool { return segs[i].gen < segs[j].gen })
	}
	return final, segs, true
}

// hasComment reports whether a line may hold a comment.
func hasComment(line []byte) bool {
	return bytes.Contains(line, []byte("//")) || bytes.Contains(line, []byte("/*"))
}

// gofmt prints as go/format does, number literals normalized too (its
// printerNormalizeNumbers); format.Node would print and parse a file with an
// import block once more to sort it.
var gofmt = printer.Config{Mode: printer.UseSpaces | printer.TabIndent | 1<<30, Tabwidth: 8}

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

// addImports puts specs in the first import block of a formatted file as
// goimports does: a standard library package into the group of the
// standard library's (a new first group when there is none), another into
// the group sharing the longest leading path with it, else the last group
// of other packages (a new last group when there is none). reformat reports
// that a group needs sorting again.
func addImports(text []byte, specs []*ast.ImportSpec) (out []byte, reformat bool, err error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", text, parser.ImportsOnly)
	if err != nil {
		return nil, false, err
	}
	have := map[string]bool{}
	for _, s := range file.Imports {
		have[importText(s)] = true
	}
	var lines, paths []string
	for _, s := range specs {
		if l := importText(s); !have[l] {
			have[l] = true
			lines = append(lines, l)
			paths = append(paths, s.Path.Value)
		}
	}
	if len(lines) == 0 {
		return text, false, nil
	}
	sort.Sort(byPath{lines, paths})
	tf := fset.File(file.Pos())
	if len(file.Imports) == 0 {
		var std, other []string
		for i, l := range lines {
			if p, _ := strconv.Unquote(paths[i]); isStd(p) {
				std = append(std, l)
			} else {
				other = append(other, l)
			}
		}
		block := strings.Join(std, "\n\t")
		if len(std) > 0 && len(other) > 0 {
			block += "\n\n\t"
		}
		block += strings.Join(other, "\n\t")
		at := tf.Offset(file.Name.End())
		out = append(out, text[:at]...)
		out = append(out, "\n\nimport (\n\t"+block+"\n)"...)
		return append(out, text[at:]...), len(std) > 0 && len(other) > 0, nil
	}
	g := file.Decls[0].(*ast.GenDecl)
	if !g.Lparen.IsValid() {
		spec := string(text[tf.Offset(g.Specs[0].Pos()):tf.Offset(g.End())])
		text = []byte(string(text[:tf.Offset(g.TokPos)]) + "import (\n\t" + spec + "\n)" + string(text[tf.Offset(g.End()):]))
		return addImports(text, specs)
	}
	// the groups: runs of specs on consecutive lines
	type group struct {
		end   int // offset of the end of its last line
		paths []string
	}
	var groups []*group
	last := -1
	for _, sp := range g.Specs {
		is := sp.(*ast.ImportSpec)
		line := tf.Line(is.Pos())
		p, _ := strconv.Unquote(is.Path.Value)
		if last < 0 || line > last+1 {
			groups = append(groups, &group{})
		}
		gr := groups[len(groups)-1]
		gr.end = tf.Offset(is.End())
		if is.Comment != nil {
			gr.end = tf.Offset(is.Comment.End())
		}
		gr.paths = append(gr.paths, p)
		last = tf.Line(is.End())
	}
	type insert struct {
		at   int
		text string
	}
	var ins []insert
	var first, tail []string // new groups before the first and after the last
	for i, l := range lines {
		p, _ := strconv.Unquote(paths[i])
		best, n := -1, 0
		for j, gr := range groups {
			for _, q := range gr.paths {
				switch {
				case isStd(p) && isStd(q):
					if n == 0 {
						best, n = j, 1
					}
				case !isStd(p) && !isStd(q):
					if k := sharedPath(p, q) + 1; k > n || k == n && k == 1 {
						best, n = j, k
					}
				}
			}
		}
		switch {
		case best >= 0:
			ins = append(ins, insert{groups[best].end, "\n\t" + l})
			reformat = true
		case isStd(p):
			first = append(first, l)
		default:
			tail = append(tail, l)
		}
	}
	if len(first) > 0 {
		ins = append(ins, insert{tf.Offset(g.Lparen) + 1, "\n\t" + strings.Join(first, "\n\t") + "\n"})
	}
	if len(tail) > 0 {
		ins = append(ins, insert{groups[len(groups)-1].end, "\n\n\t" + strings.Join(tail, "\n\t")})
	}
	sort.SliceStable(ins, func(i, j int) bool { return ins[i].at < ins[j].at })
	at := 0
	for _, in := range ins {
		out = append(out, text[at:in.at]...)
		out = append(out, in.text...)
		at = in.at
	}
	return append(out, text[at:]...), reformat, nil
}

// sharedPath is how many leading path elements p and q share.
func sharedPath(p, q string) int {
	a, b := strings.Split(p, "/"), strings.Split(q, "/")
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return n
}

// byPath sorts import lines by their quoted paths.
type byPath struct{ lines, paths []string }

func (b byPath) Len() int           { return len(b.lines) }
func (b byPath) Less(i, j int) bool { return b.paths[i] < b.paths[j] }
func (b byPath) Swap(i, j int) {
	b.lines[i], b.lines[j] = b.lines[j], b.lines[i]
	b.paths[i], b.paths[j] = b.paths[j], b.paths[i]
}

// importText is an import spec as a line of an import block: the name only
// when it isn't the package's own.
func importText(s *ast.ImportSpec) string {
	path, _ := strconv.Unquote(s.Path.Value)
	if s.Name == nil || s.Name.Name == ImportName(path) {
		return strconv.Quote(path)
	}
	return s.Name.Name + " " + strconv.Quote(path)
}

// ImportName is the name an import of path brings into scope when the
// package follows the convention: the path's last element, or the element
// before a major-version suffix (ui for github.com/vuka-lang/ui/v2).
func ImportName(path string) string {
	i := strings.LastIndexByte(path, '/')
	last := path[i+1:]
	if i > 0 && isMajorSuffix(last) {
		prev := path[:i]
		return prev[strings.LastIndexByte(prev, '/')+1:]
	}
	return last
}

// isMajorSuffix reports whether elem is a module path's major-version
// element: v2, v3, ….
func isMajorSuffix(elem string) bool {
	n, ok := strings.CutPrefix(elem, "v")
	if !ok || n == "" || n[0] == '0' || n == "1" {
		return false
	}
	for _, r := range n {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// renameLabels renames the {__name} holes of a template literal, as the
// variables they label are renamed.
func renameLabels(lit string, renames map[string]string) string {
	var b strings.Builder
	for {
		i := strings.Index(lit, "{__")
		if i < 0 {
			b.WriteString(lit)
			return b.String()
		}
		j := i + 1
		for j < len(lit) && (lit[j] == '_' || unicode.IsLetter(rune(lit[j])) || unicode.IsDigit(rune(lit[j]))) {
			j++
		}
		b.WriteString(lit[:i+1])
		if r, ok := renames[lit[i+1:j]]; ok {
			b.WriteString(r)
		} else {
			b.WriteString(lit[i+1 : j])
		}
		lit = lit[j:]
	}
}
