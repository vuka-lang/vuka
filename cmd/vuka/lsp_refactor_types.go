package main

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// typedPkg is a package of the module type-checked from its generated Go,
// for the refactors that need a type: what a name in a .vuka file refers to,
// and its type.
type typedPkg struct {
	root, modPath string
	dir           string // the source directory
	fset          *token.FileSet
	pkg           *types.Package
	info          *types.Info
	files         map[string]*typedFile // by the .vuka file
}

// typedFile is a .vuka file's generated Go, parsed, and its source map.
type typedFile struct {
	from []byte // the source it was generated from
	ast  *ast.File
	tf   *token.File
	m    *transpile.SourceMap
}

// loadTyped transpiles the package of the .vuka file path (and what it
// imports of the module) with the editor's text, and type-checks it. Type
// errors are tolerated: what checks still has its types.
func loadTyped(path string, read load.ReadFunc) (*typedPkg, error) {
	dir := filepath.Dir(path)
	root, modPath, err := load.ModuleRoot(dir)
	if err != nil {
		return nil, err
	}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	all, err := load.Discover(root, modPath, root, true, read)
	if err != nil {
		return nil, err
	}
	var own *load.Package
	for _, p := range all {
		if p.Dir == dir {
			own = p
		}
	}
	if own == nil {
		return nil, errors.New("no package in " + dir)
	}
	tmp, err := os.MkdirTemp("", "vuka-refactor-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	gens, overlay, _ := load.Transpile(load.Closure(all, []*load.Package{own}), tmp, load.Options{Bare: true})
	tp := &typedPkg{root: root, modPath: modPath, dir: dir, fset: token.NewFileSet(), files: map[string]*typedFile{}}
	var files []*ast.File
	generated := map[string]bool{}
	for _, g := range gens {
		mine := g.Source != "" && filepath.Dir(g.Source) == dir || g.Source == "" && filepath.Dir(g.Target) == own.BuildDir()
		if !mine {
			continue
		}
		generated[filepath.Base(g.Target)] = true
		f, err := parser.ParseFile(tp.fset, g.Target, g.Src, parser.SkipObjectResolution)
		if f == nil {
			return nil, err
		}
		files = append(files, f)
		if g.Source != "" && g.Map != nil {
			tp.files[g.Source] = &typedFile{from: g.From, ast: f, tf: tp.fset.File(f.Pos()), m: g.Map}
		}
	}
	if len(tp.files) == 0 {
		return nil, errors.New(dir + " doesn't transpile")
	}
	for _, f := range own.Files {
		if f.IsVuka() || generated[f.Name] || strings.HasSuffix(f.Name, "_test.go") || !strings.HasSuffix(f.Name, ".go") {
			continue
		}
		if af, _ := parser.ParseFile(tp.fset, filepath.Join(dir, f.Name), f.Src, parser.SkipObjectResolution); af != nil {
			files = append(files, af)
		}
	}
	tp.info = &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}, Types: map[ast.Expr]types.TypeAndValue{}}
	conf := types.Config{Importer: load.NewImporter(root, overlay), Error: func(error) {}}
	importPath := modPath
	if rel, err := filepath.Rel(root, own.BuildDir()); err == nil && rel != "." {
		importPath += "/" + filepath.ToSlash(rel)
	}
	tp.pkg, _ = conf.Check(importPath, tp.fset, files, tp.info)
	return tp, nil
}

// sourceIdent is a name the source spells, as the generated Go uses it.
type sourceIdent struct {
	id  *ast.Ident
	off int // in the source
	obj types.Object
}

// idents are the names of f's generated Go that the source spells in
// [start, end), by where the source has them; a name the generated Go uses
// twice for one place counts once.
func (tp *typedPkg) idents(f *typedFile, start, end int) []sourceIdent {
	var out []sourceIdent
	seen := map[int]bool{}
	ast.Inspect(f.ast, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		off, ok := f.sourceOf(id)
		if !ok || off < start || off >= end || seen[off] {
			return true
		}
		obj := tp.info.Uses[id]
		if obj == nil {
			obj = tp.info.Defs[id]
		}
		if obj == nil {
			return true
		}
		seen[off] = true
		out = append(out, sourceIdent{id: id, off: off, obj: obj})
		return true
	})
	return out
}

// sourceOf is where the source spells the generated name id: false when
// what is there is another name (Vuka wrote it: a bare field's receiver, a
// tracked field's Get).
func (f *typedFile) sourceOf(id *ast.Ident) (int, bool) {
	gen := f.tf.Offset(id.Pos())
	off, _, ok := f.m.ToSource(gen)
	if !ok || off+len(id.Name) > len(f.from) || string(f.from[off:off+len(id.Name)]) != id.Name {
		return 0, false
	}
	if off+len(id.Name) < len(f.from) && isIdentByte(f.from[off+len(id.Name)]) || off > 0 && isIdentByte(f.from[off-1]) {
		return 0, false
	}
	return off, true
}

// declSource is where the source declares obj, in f: false when Vuka
// declared it, or another file does.
func (tp *typedPkg) declSource(f *typedFile, obj types.Object) (int, bool) {
	if !obj.Pos().IsValid() || tp.fset.File(obj.Pos()) != f.tf {
		return 0, false
	}
	id := &ast.Ident{NamePos: obj.Pos(), Name: obj.Name()}
	return f.sourceOf(id)
}

// shapeType is the named type the shape file's struct is.
func (tp *typedPkg) shapeType(name string) *types.Named {
	if tp.pkg == nil {
		return nil
	}
	tn, _ := tp.pkg.Scope().Lookup(name).(*types.TypeName)
	if tn == nil {
		return nil
	}
	n, _ := tn.Type().(*types.Named)
	return n
}

// renderRecvs are the receivers of the methods Vuka writes for the shape
// type (Render, View): a bare field or method in the markup is a selector on
// one of them.
func (tp *typedPkg) renderRecvs(f *typedFile, shape *types.Named) map[types.Object]bool {
	out := map[types.Object]bool{}
	for _, d := range f.ast.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 || len(fd.Recv.List[0].Names) != 1 {
			continue
		}
		if _, own := f.sourceOf(fd.Name); own {
			continue // the header's
		}
		obj := tp.info.Defs[fd.Recv.List[0].Names[0]]
		if obj == nil || !sameNamed(obj.Type(), shape) {
			continue
		}
		out[obj] = true
	}
	return out
}

// sameNamed reports whether t, through a pointer, is n.
func sameNamed(t types.Type, n *types.Named) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	tn, ok := t.(*types.Named)
	return ok && n != nil && tn.Obj() == n.Obj()
}

// selectorsOf maps each selector's name in f to the selector.
func selectorsOf(f *ast.File) map[*ast.Ident]*ast.SelectorExpr {
	out := map[*ast.Ident]*ast.SelectorExpr{}
	ast.Inspect(f, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			out[s.Sel] = s
		}
		return true
	})
	return out
}
