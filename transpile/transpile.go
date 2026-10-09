// Package transpile turns Vuka source into Go.
//
// Vuka is Go plus a small set of constructs. Each one is found at token level and
// lowered to plain Go; everything else passes through untouched, to be parsed and
// type-checked by the installed Go toolchain's own go/parser and go/types. So a
// new Go release's syntax works in Vuka without a Vuka release.
package transpile

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
)

// RuntimePath is the import path of the package holding Result and Option.
const RuntimePath = "github.com/vuka-lang/vuka"

// RuntimeVersion is the runtime version the code Vuka generates needs.
const RuntimeVersion = "v0.3.0"

// File is one source file of a package: a .vuka file to transpile, or a .go file
// in the same package.
type File struct {
	Name string // base name: "shapes.vuka", "util.go"
	Src  []byte
}

// IsVuka reports whether f is Vuka source.
func (f File) IsVuka() bool { return strings.HasSuffix(f.Name, ".vuka") }

// Options configure Package.
type Options struct {
	// Importer resolves imports when a package has to be type-checked
	// (overloads, ?, match, Result and Option). Nil uses go/importer's source
	// importer.
	Importer types.Importer
	// Path maps a file name to the path written into //line directives and
	// errors. Nil uses the name.
	Path func(name string) string
	// Bare emits no header and no line directives, so a .vuka file without Vuka
	// constructs comes out byte-identical.
	Bare bool
}

// Output is the Go generated for one .vuka file.
type Output struct {
	Name   string // the .vuka file
	GoName string // the generated file, GoName(Name)
	Src    []byte
	Map    *SourceMap
	// Body is where the source's own lines end in Src. Before it, line n of
	// Src is line n of the .vuka file (plus the header when not bare); after
	// it comes code Vuka adds: attribute checks, statics, wrappers.
	Body int
}

// Result is a transpiled package.
type Result struct {
	Files []Output
	Attrs []Attr // every attribute, in file order
}

// GoName is the name of the Go file generated for a .vuka file.
func GoName(name string) string {
	base := strings.TrimSuffix(name, ".vuka")
	if strings.HasSuffix(base, "_test") {
		return strings.TrimSuffix(base, "_test") + "_vuka_test.go"
	}
	return base + "_vuka.go"
}

type fileState struct {
	name, path string
	src        []byte
	vuka       bool
	lines      lineIndex

	attrs   []*Attr
	tries   []*try
	matches []*matchStmt
	jsx     []*jsxTree

	fixed    edits     // decided rewrites, in src offsets
	cur      edits     // this round's: fixed plus placeholders
	body     int       // length of text before the trailer
	trailer  string    // generated after the source: typed attribute checks, decorator wrappers
	tsegs    []segment // where each piece of the trailer comes from
	deco     genWriter // the decorator wrappers
	decoBase int       // where they start in the trailer
	decos    []any     // *funcDeco and *typeDeco, in source order
	text     []byte

	ast     *ast.File
	tf      *token.File
	parents map[ast.Node]ast.Node

	done    map[int]bool // src offsets of identifiers already rewritten or reported
	rt      string       // the name the runtime is imported as; "" until needed
	pkgEnd  int          // offset just after the package clause's name
	dotImps bool

	decorated map[int]string // declaration offset → the decorated function's name

	bare        bool
	statics     []*staticDecl
	staticFuncs []*staticFunc
	static      genWriter // the statics' declarations
	staticErrs  []*Error
	autoImports map[string]bool // packages imported for statics reached through other types
}

// at is the position of a src offset.
func (f *fileState) at(off int) token.Position { return f.lines.pos(f.path, off) }

// pos is the position, in the file as written, of an offset in this round's text.
func (f *fileState) pos(off int) token.Position {
	if off >= f.body {
		rel := off - f.body
		for _, s := range f.tsegs {
			if rel >= s.gen && rel < s.gen+s.genLen {
				if s.copy {
					return f.at(s.src + rel - s.gen)
				}
				return f.at(s.src)
			}
		}
	}
	return f.at(f.cur.toOrig(min(off, f.body)))
}

func (f *fileState) off(p token.Pos) int { return f.tf.Offset(p) }

// orig is the src offset of a position in this round's AST.
func (f *fileState) orig(p token.Pos) int { return f.cur.toOrig(f.off(p)) }

func (f *fileState) nodePos(p token.Pos) token.Position { return f.pos(f.off(p)) }

func (f *fileState) nodeText(n ast.Node) string {
	return string(f.text[f.off(n.Pos()):f.off(n.End())])
}

func (f *fileState) add(start, end int, text string) {
	f.fixed = append(f.fixed, edit{start: start, end: end, text: text})
}

func (f *fileState) insert(off int, text string, prio int) {
	f.fixed = append(f.fixed, edit{start: off, end: off, text: text, prio: prio})
}

// attrEdits turns every attribute into comments in place.
func (f *fileState) attrEdits() {
	for _, a := range f.attrs {
		f.fixed = append(f.fixed, edit{start: a.start, end: a.end, text: a.replacement(f.src)})
	}
}

// makeTrailer writes what follows the source: a check that makes Go type-check
// each typed attribute, then the decorator wrappers.
func (f *fileState) makeTrailer(bare bool) {
	var w genWriter
	for _, a := range f.attrs {
		if a.kind != attrTyped {
			continue
		}
		if w.len() == 0 && len(f.src) > 0 && f.src[len(f.src)-1] != '\n' {
			w.gen("\n", a.start)
		}
		prefix, expr := a.check(f.src)
		w.gen("\n"+prefix, a.start)
		if !bare {
			p := a.Pos
			p.Column++
			w.gen(lineDirective(p), a.start)
		}
		w.copy(expr, a.nameStart)
		w.gen("\n", a.end)
	}
	if (f.deco.len() > 0 || f.static.len() > 0) && w.len() == 0 && len(f.src) > 0 && f.src[len(f.src)-1] != '\n' {
		w.gen("\n", len(f.src))
	}
	w.append(&f.static)
	f.decoBase = w.len()
	w.append(&f.deco)
	f.trailer, f.tsegs = w.String(), w.segs
}

// build produces this round's text: the decided rewrites, plus a placeholder for
// every construct not lowered yet that still lets the package type-check.
func (f *fileState) build() {
	cur := append(edits(nil), f.fixed...)
	for _, t := range f.tries {
		if !t.done {
			cur = append(cur, edit{start: t.off, end: t.off + 1})
		}
	}
	for _, m := range f.matches {
		if m.done {
			continue
		}
		v := "__m" + itoa(m.n)
		cur = append(cur,
			edit{start: m.start, end: m.subj.start, text: "if " + v + " := "},
			edit{start: m.subj.end, end: m.lbrace + 1, text: "; false { _ = " + v + ";"})
		for _, c := range m.cases {
			cur = append(cur, edit{start: c.start, end: c.colon + 1, text: "} else if false {"})
		}
	}
	for _, t := range f.jsx {
		if !t.done {
			cur = append(cur, f.jsxEdits(t)...)
		}
	}
	f.cur = cur.sorted()
	body := f.cur.apply(f.src, nil)
	f.body = len(body)
	f.text = append(body, f.trailer...)
}

func (f *fileState) parse(fset *token.FileSet, errs *ErrorList) bool {
	file, err := parser.ParseFile(fset, f.path, f.text, parser.ParseComments)
	if err != nil {
		if list, ok := err.(scanner.ErrorList); ok {
			for _, e := range list {
				errs.add(f.pos(e.Pos.Offset), "%s", e.Msg)
			}
		} else {
			errs.add(f.at(0), "%v", err)
		}
		return false
	}
	f.ast, f.tf = file, fset.File(file.Pos())
	f.parents = map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(file, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		if len(stack) > 0 {
			f.parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	return true
}

// attach finds the declaration each attribute annotates.
func (f *fileState) attach() {
	for _, a := range f.attrs {
		off := f.cur.fromOrig(a.declOff)
		for _, d := range f.ast.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if f.off(d.Type.Func) == off {
					a.decl, a.Decl = d, declKey(d)
				}
			case *ast.GenDecl:
				if f.off(d.TokPos) == off {
					a.decl, a.Decl = d, genName(d)
				}
			}
		}
	}
}

func genName(d *ast.GenDecl) string {
	if len(d.Specs) == 0 {
		return ""
	}
	switch s := d.Specs[0].(type) {
	case *ast.TypeSpec:
		return s.Name.Name
	case *ast.ValueSpec:
		return s.Names[0].Name
	}
	return ""
}

func (f *fileState) emit(bare bool, extra string, extraSegs []segment) ([]byte, *SourceMap, int) {
	all := f.fixed.sorted()
	var after func(int) string
	if !bare {
		after = func(off int) string { return lineDirective(f.at(off)) }
	}
	var b bytes.Buffer
	m := &SourceMap{}
	if !bare {
		fmt.Fprintf(&b, "// Code generated by vuka from %s. DO NOT EDIT.\n\n//line %s:1:1\n", filepath.Base(f.name), f.path)
		m.add(0, []segment{{genLen: b.Len()}})
	}
	body, segs := all.applyMap(f.src, after)
	m.add(b.Len(), segs)
	b.Write(body)
	bodyEnd := b.Len()
	m.add(b.Len(), f.tsegs)
	b.WriteString(f.trailer)
	m.add(b.Len(), extraSegs)
	b.WriteString(extra)
	return b.Bytes(), m, bodyEnd
}

// Package transpiles one package: its .vuka files, plus any .go files that share
// the package (read for type information, never rewritten).
func Package(files []File, opts Options) (*Result, error) {
	var errs ErrorList
	e := &engine{imp: opts.Importer, errs: &errs, bare: opts.Bare}
	for _, file := range files {
		f := &fileState{name: file.Name, path: file.Name, src: file.Src, vuka: file.IsVuka(),
			lines: newLineIndex(file.Src), done: map[int]bool{}, bare: opts.Bare}
		if opts.Path != nil {
			f.path = opts.Path(file.Name)
		}
		if f.vuka {
			f.scan(&errs)
			f.attrEdits()
			f.makeTrailer(opts.Bare)
			e.vuka = append(e.vuka, f)
		}
		e.files = append(e.files, f)
	}
	if err := errs.err(); err != nil {
		return nil, err
	}
	if len(e.vuka) == 0 {
		return &Result{}, nil
	}
	e.run()
	if err := errs.err(); err != nil {
		return nil, err
	}
	if !e.parseAll(false) {
		return nil, errs.err()
	}
	for _, f := range e.vuka {
		f.attach()
	}

	res := &Result{}
	for _, f := range e.vuka {
		extra, segs := f.exports(opts.Bare, &errs)
		src, m, body := f.emit(opts.Bare, extra, segs)
		res.Files = append(res.Files, Output{Name: f.name, GoName: GoName(f.name), Src: src, Map: m, Body: body})
		for _, a := range f.attrs {
			res.Attrs = append(res.Attrs, *a)
		}
	}
	if err := errs.err(); err != nil {
		return nil, err
	}
	return res, nil
}
