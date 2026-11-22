package transpile

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
)

// ImportUses is what a file's code names by a package name: the names its
// code qualifies (strings in strings.ToUpper, orm in @orm.PK, components in
// <components.Logo>), each where the file first writes it.
type ImportUses struct {
	// Qualifiers are the names the file's code uses as a package, by where
	// it first writes them (a source offset): those no declaration of the
	// file resolves, in its Go, its markup's holes, attributes and
	// component tags, and its attributes.
	Qualifiers map[string]int
	// Attrs are the names its attributes qualify (orm in @orm.PK).
	Attrs map[string]bool
	// Tags are the names the markup's component tags use bare (Counter in
	// <Counter/>): a use of an import of that name — an island's .vue, a
	// component package's — though never a package to add.
	Tags map[string]bool
}

// Uses reads which package names the file's code uses: the code Vuka's
// syntax lowers to, so markup, attributes and sugar count as written, and
// nothing Vuka adds does. It needs no type-check; the error is the file's
// syntax, when its code doesn't parse. opts and others are as for Syntax.
func Uses(file File, opts Options, others ...File) (*ImportUses, error) {
	var errs ErrorList
	e := &engine{imp: opts.Importer, errs: &errs, bare: true, dir: opts.Dir, moduleRoot: opts.ModuleRoot, importPath: opts.ImportPath, scopes: opts.Scopes}
	if e.imp == nil {
		e.imp = noImporter{}
	}
	all := e.headerViews(append([]File{file}, others...), opts.Path, 1)
	if err := errs.err(); err != nil {
		return nil, err
	}
	file = all[0]
	f := &fileState{name: file.Name, path: file.Name, src: file.Src, vuka: true,
		lines: newLineIndex(file.Src), done: map[int]bool{}, bare: true, keepGuards: true, eng: e}
	e.detectSelfTarget(all)
	if file.IsTemplate() && !e.template(f, all) {
		return nil, errs.err()
	}
	errs = errs[:0]
	f.scan(&errs)
	if err := syntaxOnly(errs); err != nil {
		return nil, err
	}
	f.attrEdits()
	f.makeTrailer(true)
	f.build()
	if !f.parse(token.NewFileSet(), &errs) {
		return nil, errs.err()
	}
	src := file.Src
	toSrc := func(off int) int {
		if f.html != nil {
			return f.html.toSrc(off)
		}
		return off
	}
	u := &ImportUses{Qualifiers: map[string]int{}, Attrs: map[string]bool{}, Tags: map[string]bool{}}
	add := func(name string, at int) {
		if prev, ok := u.Qualifiers[name]; !ok || at < prev {
			u.Qualifiers[name] = at
		}
	}
	ast.Inspect(f.ast, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok || x.Obj != nil || strings.HasPrefix(x.Name, "__") {
			return true
		}
		off := f.orig(x.Pos())
		at := toSrc(off)
		if at < 0 || at+len(x.Name) > len(src) || string(src[at:at+len(x.Name)]) != x.Name {
			return true // Vuka wrote it
		}
		if f.shapeName(off, x.Name) {
			return true // the markup's own field (user in {user.Name})
		}
		if matchBound(f, off, x.Name) {
			return true // a pattern's binding read by its guard
		}
		add(x.Name, at)
		return true
	})
	for _, t := range f.jsx {
		for _, c := range t.comps {
			if tag := c.el.tag; !strings.Contains(tag, ".") {
				u.Tags[tag] = true
			}
		}
	}
	for _, a := range f.attrs {
		if pkg, _, ok := strings.Cut(a.Name, "."); ok && pkg != "vuka" {
			add(pkg, attrOffset(src, a.Pos))
			u.Attrs[pkg] = true
		}
	}
	return u, nil
}

// matchBound reports whether name at off (an f.src offset) is inside a match
// case's guard and bound by that case's pattern: u in `case u if u.Admin:`.
func matchBound(f *fileState, off int, name string) bool {
	for _, m := range f.matches {
		for _, c := range m.cases {
			if c.guard.start < 0 || off < c.guard.start || off >= c.guard.end {
				continue
			}
			for _, sp := range c.pats {
				x, err := parser.ParseExpr(string(f.src[sp.start:sp.end]))
				if err != nil {
					continue
				}
				bound := false
				ast.Inspect(x, func(n ast.Node) bool {
					if id, ok := n.(*ast.Ident); ok && id.Name == name {
						bound = true
					}
					return !bound
				})
				if bound {
					return true
				}
			}
		}
	}
	return false
}

// syntaxOnly is errs' first error that stops the file being read: the
// errors of a lowering that needs the package's types don't.
func syntaxOnly(errs ErrorList) error {
	for _, e := range errs {
		if e.Code == "E0003" {
			return errs
		}
	}
	return nil
}

// attrOffset is the offset of an attribute's @ in src.
func attrOffset(src []byte, p token.Position) int {
	off := 0
	for line := 1; line < p.Line && off < len(src); line++ {
		i := strings.IndexByte(string(src[off:]), '\n')
		if i < 0 {
			return len(src)
		}
		off += i + 1
	}
	return min(off+p.Column-1, len(src))
}

// noImporter loads no package: reading uses needs no type.
type noImporter struct{}

func (noImporter) Import(path string) (*types.Package, error) {
	return nil, errors.New("not loaded: " + path)
}
