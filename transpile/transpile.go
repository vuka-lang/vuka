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
	"go/importer"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
)

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
	// Importer resolves imports when a package overloads functions and has to be
	// type-checked. Nil uses go/importer's source importer.
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
	edits   edits // attribute rewrites, in src offsets
	inter   []byte
	body    int // length of inter before the trailer
	trailer string
	snips   []snippet

	ast     *ast.File
	tf      *token.File
	renames edits // overload renames, in src offsets
}

// snippet is a typed attribute's check in the trailer.
type snippet struct {
	start, end int
	attr       *Attr
}

// at is the position of a src offset.
func (f *fileState) at(off int) token.Position { return f.lines.pos(f.path, off) }

// pos is the position, in the file as written, of an offset in the intermediate text.
func (f *fileState) pos(off int) token.Position {
	if off >= f.body {
		for _, s := range f.snips {
			if off < s.end {
				return s.attr.Pos
			}
		}
	}
	return f.at(f.edits.toOrig(min(off, f.body)))
}

func (f *fileState) nodePos(p token.Pos) token.Position { return f.pos(f.tf.Offset(p)) }

func (f *fileState) text(n ast.Node) string {
	return string(f.inter[f.tf.Offset(n.Pos()):f.tf.Offset(n.End())])
}

// build applies the attribute rewrites and appends the typed attributes' checks.
func (f *fileState) build(bare bool) {
	for _, a := range f.attrs {
		f.edits = append(f.edits, edit{a.start, a.end, a.replacement(f.src)})
	}
	f.edits = f.edits.sorted()
	body := f.edits.apply(f.src, nil)
	f.body = len(body)
	var t strings.Builder
	for _, a := range f.attrs {
		if a.kind != attrTyped {
			continue
		}
		if t.Len() == 0 && len(body) > 0 && body[len(body)-1] != '\n' {
			t.WriteByte('\n')
		}
		start := f.body + t.Len()
		prefix, expr := a.check(f.src)
		t.WriteString("\n" + prefix)
		if !bare {
			p := a.Pos
			p.Column++
			t.WriteString(lineDirective(p))
		}
		t.WriteString(expr + "\n")
		f.snips = append(f.snips, snippet{start, f.body + t.Len(), a})
	}
	f.trailer = t.String()
	f.inter = append(body, f.trailer...)
}

func (f *fileState) parse(fset *token.FileSet, errs *ErrorList) bool {
	file, err := parser.ParseFile(fset, f.path, f.inter, parser.ParseComments|parser.SkipObjectResolution)
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
	return true
}

// attach finds the declaration each attribute annotates.
func (f *fileState) attach() {
	for _, a := range f.attrs {
		off := f.edits.fromOrig(a.declOff)
		for _, d := range f.ast.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if f.tf.Offset(d.Type.Func) == off {
					a.decl, a.Decl = d, declKey(d)
				}
			case *ast.GenDecl:
				if f.tf.Offset(d.TokPos) == off {
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

// rename records that ident, written as old, becomes name.
func (f *fileState) rename(id *ast.Ident, old, name string, errs *ErrorList) {
	off := f.tf.Offset(id.Pos())
	if off >= f.body {
		errs.add(f.pos(off), "an overloaded call can't appear inside an attribute")
		return
	}
	start := f.edits.toOrig(off)
	f.renames = append(f.renames, edit{start, start + len(old), name})
	id.Name = name
}

func (f *fileState) emit(bare bool, extra string) []byte {
	all := append(append(edits(nil), f.edits...), f.renames...).sorted()
	var after func(int) string
	if !bare {
		after = func(off int) string { return lineDirective(f.at(off)) }
	}
	var b bytes.Buffer
	if !bare {
		fmt.Fprintf(&b, "// Code generated by vuka from %s. DO NOT EDIT.\n\n//line %s:1:1\n", filepath.Base(f.name), f.path)
	}
	b.Write(all.apply(f.src, after))
	b.WriteString(f.trailer)
	b.WriteString(extra)
	return b.Bytes()
}

// Package transpiles one package: its .vuka files, plus any .go files that share
// the package (read for type information, never rewritten).
func Package(files []File, opts Options) (*Result, error) {
	var errs ErrorList
	var all, vuka []*fileState
	for _, file := range files {
		f := &fileState{name: file.Name, path: file.Name, src: file.Src, vuka: file.IsVuka(), lines: newLineIndex(file.Src)}
		if opts.Path != nil {
			f.path = opts.Path(file.Name)
		}
		if f.vuka {
			f.scanAttrs(&errs)
			vuka = append(vuka, f)
		} else {
			f.inter, f.body = f.src, len(f.src)
		}
		all = append(all, f)
	}
	if err := errs.err(); err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	for _, f := range vuka {
		f.build(opts.Bare)
		if f.parse(fset, &errs) {
			f.attach()
		}
	}
	if err := errs.err(); err != nil {
		return nil, err
	}

	if sets := findOverloads(vuka, &errs); len(sets) > 0 && len(errs) == 0 {
		for _, f := range all {
			if !f.vuka {
				f.parse(fset, &errs)
			}
		}
		if len(errs) == 0 {
			imp := opts.Importer
			if imp == nil {
				imp = importer.ForCompiler(fset, "source", nil)
			}
			resolve(fset, all, sets, imp, &errs)
		}
	}
	if err := errs.err(); err != nil {
		return nil, err
	}

	res := &Result{}
	for _, f := range vuka {
		extra := f.exports(opts.Bare, &errs)
		res.Files = append(res.Files, Output{Name: f.name, GoName: GoName(f.name), Src: f.emit(opts.Bare, extra)})
		for _, a := range f.attrs {
			res.Attrs = append(res.Attrs, *a)
		}
	}
	if err := errs.err(); err != nil {
		return nil, err
	}
	return res, nil
}
