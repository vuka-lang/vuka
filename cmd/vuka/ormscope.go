package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// A watch rebuild asks the orm tool what the module's models generate. The
// tool loads every package calling orm.For or declaring a model, which in an
// app whose pages all import one models package is most of the module. An
// edit changes the generated code of few directories, so the module's answer
// is kept by directory and only the directories an edit can reach are asked
// again: the tool, given a directory, loads the packages under it alone.
//
// The answer for a directory of models depends on the Go of:
//   - its own files, and the packages it imports, directly or not (the types
//     of its fields, embedded structs, the models its relations lead to);
//   - the models of packages importing it (their relations' Related names
//     are its reverse relations), with what those import;
//   - every orm.For[T] naming one of its types: T becomes a model. Only a
//     T that embeds orm.Model[T] (or Base, Rec) is one anyway.
//
// So an edit's directories reach the models directories whose answer can
// change, the run must load what those answers read, and the tool is run in
// the smallest directory holding all of it. When that is the module's root,
// or something can't be told from the source (a file that doesn't parse, a
// For naming an alias), the whole module is asked, as before.

// ormFileFacts is what of a Go file decides which directories' orm answers
// it reaches. Selectors are kept by the name the file uses, resolved against
// the module's packages when planning.
type ormFileFacts struct {
	Broken  bool // doesn't parse: nothing is known
	ORM     bool // the tool loads its package (ormgen's callsFor)
	Pkg     string
	Imports []ormImport
	Types   []ormTypeFacts
	Targets []ormExpr // the type arguments of For[…]
	Opaque  bool      // a For[…] whose type isn't a name
}

type ormImport struct{ Name, Path string } // Name "" when the file gives none

// ormTypeFacts is a type declaration: whether it is an alias, generic, a
// model by embedding orm.Model of itself (Self), and the types its struct
// embeds, or the type it is defined as, through which it may be one.
type ormTypeFacts struct {
	Name                 string
	Alias, Generic, Self bool
	Embeds               []ormExpr
	Opaque               bool // an embedded type that isn't a name
}

// ormExpr is a named type: X.Name, or Name when X is "". Generic when
// instantiated.
type ormExpr struct {
	X, Name string
	Generic bool
}

func ormFactsOf(f *ast.File, src []byte) *ormFileFacts {
	ff := &ormFileFacts{Pkg: f.Name.Name, ORM: ormCallsFor(src)}
	ormName := ""
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name
		}
		ff.Imports = append(ff.Imports, ormImport{name, path})
		if path == ormModule {
			ormName = cmpOr(name, "orm")
		}
	}
	for _, decl := range f.Decls {
		g, ok := decl.(*ast.GenDecl)
		if !ok || g.Tok != token.TYPE {
			continue
		}
		for _, spec := range g.Specs {
			ts := spec.(*ast.TypeSpec)
			t := ormTypeFacts{Name: ts.Name.Name, Alias: ts.Assign.IsValid(), Generic: ts.TypeParams != nil}
			if t.Alias || t.Generic {
				ff.Types = append(ff.Types, t)
				continue
			}
			add := func(e ast.Expr) {
				if x, ok := ormNamed(e); ok {
					t.Embeds = append(t.Embeds, x)
				} else {
					t.Opaque = true
				}
			}
			switch st := ts.Type.(type) {
			case *ast.StructType:
				for _, field := range st.Fields.List {
					if len(field.Names) > 0 {
						continue
					}
					add(field.Type)
					t.Self = t.Self || ormSelfEmbed(field.Type, ormName, ts.Name.Name)
				}
			case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr, *ast.ParenExpr:
				add(st)
			}
			ff.Types = append(ff.Types, t)
		}
	}
	if bytes.Contains(src, []byte("For[")) {
		ast.Inspect(f, func(n ast.Node) bool {
			var fun ast.Expr
			var args []ast.Expr
			switch ix := n.(type) {
			case *ast.IndexExpr:
				fun, args = ix.X, []ast.Expr{ix.Index}
			case *ast.IndexListExpr:
				fun, args = ix.X, ix.Indices
			default:
				return true
			}
			if name := forName(fun); name != "For" {
				return true
			}
			for _, a := range args {
				if x, ok := ormNamed(a); ok {
					ff.Targets = append(ff.Targets, x)
				} else if !ormUnnamed(a) {
					ff.Opaque = true
				}
			}
			return true
		})
	}
	return ff
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func forName(e ast.Expr) string {
	switch f := e.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

// ormNamed is the named type e is, through pointers and parentheses.
func ormNamed(e ast.Expr) (ormExpr, bool) {
	generic := false
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
			continue
		case *ast.ParenExpr:
			e = x.X
			continue
		case *ast.IndexExpr:
			e, generic = x.X, true
			continue
		case *ast.IndexListExpr:
			e, generic = x.X, true
			continue
		case *ast.Ident:
			return ormExpr{Name: x.Name, Generic: generic}, true
		case *ast.SelectorExpr:
			if id, ok := x.X.(*ast.Ident); ok {
				return ormExpr{X: id.Name, Name: x.Sel.Name, Generic: generic}, true
			}
		}
		return ormExpr{}, false
	}
}

// ormUnnamed reports whether e is a type literal, which the tool takes for
// no model.
func ormUnnamed(e ast.Expr) bool {
	switch e.(type) {
	case *ast.ArrayType, *ast.MapType, *ast.FuncType, *ast.ChanType, *ast.InterfaceType, *ast.StructType:
		return true
	}
	return false
}

// ormSelfEmbed reports whether e is orm.Model[self] (or Base, Rec).
func ormSelfEmbed(e ast.Expr, ormName, self string) bool {
	if s, ok := e.(*ast.StarExpr); ok {
		e = s.X
	}
	ix, ok := e.(*ast.IndexExpr)
	if !ok {
		return false
	}
	if arg, ok := ix.Index.(*ast.Ident); !ok || arg.Name != self {
		return false
	}
	var base string
	switch f := ix.X.(type) {
	case *ast.SelectorExpr:
		if id, ok := f.X.(*ast.Ident); ok && ormName != "" && ormName != "." && id.Name == ormName {
			base = f.Sel.Name
		}
	case *ast.Ident:
		if ormName == "." {
			base = f.Name
		}
	}
	return base == "Model" || base == "Base" || base == "Rec"
}

// ormCallsFor is ormgen's test of whether the tool loads a file's package.
func ormCallsFor(src []byte) bool {
	return bytes.Contains(src, []byte(`"`+ormModule+`"`)) && (bytes.Contains(src, []byte("For[")) || bytes.Contains(src, []byte("Model[")) ||
		bytes.Contains(src, []byte("Base[")) || bytes.Contains(src, []byte("Rec[")))
}

// ormState is the module as the orm tool reads it: Full is sourceShapes.module
// with ormReads; Global the part of it outside the module's files.
type ormState struct {
	Global, Full string
	Dirs         map[string]*ormDirState
}

type ormDirState struct {
	Shape [sha256.Size]byte
	Files []*ormFileFacts
}

// ormModuleState is the module at root as overlay has it, by directory; nil
// when a file can't be read.
func (s *sourceShapes) ormModuleState(root, overlay string) *ormState {
	if s == nil {
		return nil
	}
	head, paths, files, ok := s.moduleFiles(root, overlay, false)
	if !ok {
		return nil
	}
	st := &ormState{Global: head, Dirs: map[string]*ormDirState{}}
	full := sha256.New()
	full.Write([]byte(head))
	dirHash := map[string][]byte{}
	for _, p := range paths {
		src, err := os.ReadFile(files[p])
		if err != nil {
			return nil
		}
		shape := s.file(src)
		line := fmt.Sprintf("%s %x\n", p, shape.of(ormReads, p))
		full.Write([]byte(line))
		dir := filepath.Dir(p)
		d := st.Dirs[dir]
		if d == nil {
			d = &ormDirState{}
			st.Dirs[dir] = d
		}
		dirHash[dir] = append(dirHash[dir], line...)
		d.Files = append(d.Files, shape.orm)
	}
	for dir, lines := range dirHash {
		st.Dirs[dir].Shape = sha256.Sum256(lines)
	}
	st.Full = fmt.Sprintf("%x", full.Sum(nil))
	return st
}

// ormPlan is what to ask the tool after old became now: in the directory
// Unit, the answers of the directories Affected; none when Affected is
// empty. Why, when not "", is why the whole module is asked.
type ormPlan struct {
	Unit     string
	Affected map[string]bool
	Why      string
}

// planORM compares the state the module's answer was given for with now's.
func planORM(root, modPath string, old, now *ormState) ormPlan {
	full := func(why string) ormPlan { return ormPlan{Why: why} }
	if old.Global != now.Global {
		return full("go.mod, go.sum or a module it builds from a directory changed")
	}
	g := newORMGraph(root, modPath, old, now)
	changed := map[string]bool{}
	for _, st := range []*ormState{old, now} {
		for dir := range st.Dirs {
			o, n := old.Dirs[dir], now.Dirs[dir]
			if o == nil || n == nil || o.Shape != n.Shape {
				changed[dir] = true
			}
		}
	}
	for dir := range changed {
		if !g.module[dir] {
			return full(dir + " isn't one of the module's packages")
		}
	}
	if why := g.build(); why != "" {
		return full(why)
	}
	// The directories whose answers an edit reaches: its own, and those of
	// the types its For[…] named before or names now.
	reached := map[string]bool{}
	for dir := range changed {
		reached[dir] = true
		was, is := g.targets[0][dir], g.targets[1][dir]
		for t := range was {
			if !is[t] {
				reached[t.dir] = true
			}
		}
		for t := range is {
			if !was[t] {
				reached[t.dir] = true
			}
		}
	}
	affected := map[string]bool{}
	reads := map[string]bool{} // what the affected answers read
	for m := range g.models {
		r := g.reads(m)
		if !slices.ContainsFunc(r, func(d string) bool { return reached[d] }) {
			continue
		}
		affected[m] = true
		for _, d := range r {
			reads[d] = true
		}
	}
	if len(affected) == 0 {
		return ormPlan{Affected: affected}
	}
	need := []string{}
	for d := range affected {
		need = append(need, d)
	}
	for d := range reads {
		if g.models[d] {
			need = append(need, d)
		}
	}
	// A For[T] making a model of a T that isn't one by embedding is read
	// only where it is written: its package must load too.
	for dir, ts := range g.targets[1] {
		for t := range ts {
			if reads[t.dir] && !g.self[t] {
				need = append(need, dir)
			}
		}
	}
	unit := commonDir(need)
	if rel, err := filepath.Rel(root, unit); err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return full("the models it reaches are across the module")
	}
	return ormPlan{Unit: unit, Affected: affected}
}

type ormTarget struct{ dir, name string }

// ormGraph is the module's packages as both states have them: an import of
// either counts, and a directory is a models directory if it is one in either.
type ormGraph struct {
	root, modPath string
	states        [2]*ormState
	module        map[string]bool                  // the directories that are packages of the module
	byPath        map[string]string                // import path → directory
	imports       map[string][]string              // directory → the directories it imports
	models        map[string]bool                  // the directories whose models the tool may generate code for
	targets       [2]map[string]map[ormTarget]bool // directory → the types its For[…] name, in each state
	self          map[ormTarget]bool               // the types that are models by embedding, now
	deps          map[string][]string              // transitive imports, memoized
}

func newORMGraph(root, modPath string, old, now *ormState) *ormGraph {
	g := &ormGraph{root: root, modPath: modPath, states: [2]*ormState{old, now}, module: map[string]bool{}, byPath: map[string]string{},
		imports: map[string][]string{}, models: map[string]bool{}, self: map[ormTarget]bool{}, deps: map[string][]string{}}
	for _, st := range g.states {
		for dir := range st.Dirs {
			if _, seen := g.module[dir]; seen {
				continue
			}
			path, ok := g.importPath(dir)
			g.module[dir] = ok
			if ok {
				g.byPath[path] = dir
			}
		}
	}
	return g
}

// importPath is the import path of the package in dir, not ok when dir isn't
// one of the module's: outside it, in a nested module, or where go and the
// tool look for none.
func (g *ormGraph) importPath(dir string) (string, bool) {
	rel, err := filepath.Rel(g.root, dir)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", false
	}
	if rel == "." {
		return g.modPath, true
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_") || part == "vendor" || part == "testdata" || part == "node_modules" {
			return "", false
		}
	}
	for d := dir; d != g.root; d = filepath.Dir(d) {
		if fileExists(filepath.Join(d, "go.mod")) {
			return "", false
		}
	}
	return g.modPath + "/" + filepath.ToSlash(rel), true
}

// build reads the facts of both states; a reason the whole module must be
// asked when they don't say enough.
func (g *ormGraph) build() string {
	pkgName := func(dir string) string {
		for _, st := range g.states {
			if d := st.Dirs[dir]; d != nil {
				for _, f := range d.Files {
					if f.Pkg != "" && !strings.HasSuffix(f.Pkg, "_test") {
						return f.Pkg
					}
				}
			}
		}
		return ""
	}
	g.targets = [2]map[string]map[ormTarget]bool{{}, {}}
	for i, st := range g.states {
		for dir, d := range st.Dirs {
			if !g.module[dir] {
				continue
			}
			// Each type's embeds are named by its own file's imports.
			type fileNames struct {
				names  map[string]string // the file's name of a package → its import path
				dots   []string
				dotted bool // dot-imports the orm or a package of the module's
			}
			type declared struct {
				t  ormTypeFacts
				in *fileNames
			}
			types := map[string]declared{}
			files := make([]*fileNames, len(d.Files))
			for k, f := range d.Files {
				if f.Broken {
					return dir + " has a file that doesn't parse"
				}
				if f.Opaque {
					return dir + " has a For[…] whose type isn't a name"
				}
				fn := &fileNames{names: map[string]string{}}
				for _, imp := range f.Imports {
					if dep, ok := g.byPath[imp.Path]; ok && !slices.Contains(g.imports[dir], dep) {
						g.imports[dir] = append(g.imports[dir], dep)
					}
					switch {
					case imp.Name == ".":
						fn.dots = append(fn.dots, imp.Path)
						_, local := g.byPath[imp.Path]
						fn.dotted = fn.dotted || local || imp.Path == ormModule
					case imp.Name == "_":
					case imp.Name != "":
						fn.names[imp.Name] = imp.Path
					case imp.Path == ormModule:
						fn.names["orm"] = imp.Path
					default:
						if dep, ok := g.byPath[imp.Path]; ok {
							fn.names[pkgName(dep)] = imp.Path
						}
					}
				}
				files[k] = fn
				for _, t := range f.Types {
					types[t.Name] = declared{t, fn}
				}
			}
			// internal: a package of the module's, or the orm's.
			internal := func(fn *fileNames, x string) (string, bool) {
				p, ok := fn.names[x]
				if !ok {
					return "", false
				}
				_, local := g.byPath[p]
				return p, local || p == ormModule
			}
			var possible func(t declared, seen map[string]bool) bool
			possible = func(t declared, seen map[string]bool) bool {
				if t.t.Opaque {
					return true
				}
				for _, e := range t.t.Embeds {
					switch {
					case e.Generic:
						return true
					case e.X != "":
						if _, ok := internal(t.in, e.X); ok {
							return true
						}
					default:
						u, ok := types[e.Name]
						if !ok {
							if t.in.dotted {
								return true
							}
							continue
						}
						if u.t.Alias || u.t.Generic {
							return true
						}
						if !seen[e.Name] {
							seen[e.Name] = true
							if possible(u, seen) {
								return true
							}
						}
					}
				}
				return false
			}
			isORM, maybeModels := false, false
			ts := map[ormTarget]bool{}
			for k, f := range d.Files {
				isORM = isORM || f.ORM
				fn := files[k]
				for _, t := range f.Types {
					if !t.Alias && !t.Generic && possible(declared{t, fn}, map[string]bool{t.Name: true}) {
						maybeModels = true
					}
					if i == 1 && t.Self {
						g.self[ormTarget{dir, t.Name}] = true
					}
				}
				for _, e := range f.Targets {
					if e.X != "" {
						p, ok := internal(fn, e.X)
						if !ok || p == ormModule {
							continue
						}
						ts[ormTarget{g.byPath[p], e.Name}] = true
						continue
					}
					ts[ormTarget{dir, e.Name}] = true
					for _, p := range fn.dots {
						if dep, ok := g.byPath[p]; ok {
							ts[ormTarget{dep, e.Name}] = true
						}
					}
				}
			}
			if isORM && maybeModels {
				g.models[dir] = true
			}
			g.targets[i][dir] = ts
		}
	}
	for i := range g.targets {
		for _, ts := range g.targets[i] {
			for t := range ts {
				g.models[t.dir] = true
				for _, st := range g.states {
					if d := st.Dirs[t.dir]; d != nil {
						for _, f := range d.Files {
							for _, ty := range f.Types {
								if ty.Name == t.name && ty.Alias {
									return "For[" + t.name + "] names an alias"
								}
							}
						}
					}
				}
			}
		}
	}
	return ""
}

// depsOf is the directories dir imports, directly or not.
func (g *ormGraph) depsOf(dir string) []string {
	if d, ok := g.deps[dir]; ok {
		return d
	}
	seen := map[string]bool{dir: true}
	var out []string
	stack := []string{dir}
	for len(stack) > 0 {
		d := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, dep := range g.imports[d] {
			if !seen[dep] {
				seen[dep] = true
				out = append(out, dep)
				stack = append(stack, dep)
			}
		}
	}
	g.deps[dir] = out
	return out
}

// reads is what the answer of the models directory m depends on: m, what it
// imports, and each models directory importing it, with what that imports.
func (g *ormGraph) reads(m string) []string {
	set := map[string]bool{m: true}
	for _, d := range g.depsOf(m) {
		set[d] = true
	}
	for o := range g.models {
		if o == m || !slices.Contains(g.depsOf(o), m) {
			continue
		}
		set[o] = true
		for _, d := range g.depsOf(o) {
			set[d] = true
		}
	}
	out := make([]string, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	return out
}

// commonDir is the deepest directory holding every one of dirs.
func commonDir(dirs []string) string {
	if len(dirs) == 0 {
		return ""
	}
	common := dirs[0]
	for _, d := range dirs[1:] {
		for common != d && !strings.HasPrefix(d, common+string(filepath.Separator)) {
			parent := filepath.Dir(common)
			if parent == common {
				return common
			}
			common = parent
		}
	}
	return common
}

// mergeORM is the module's answer after an edit: the fresh answers of the
// directories affected, the kept ones of every other.
func mergeORM(kept, fresh []load.Generated, affected map[string]bool) []load.Generated {
	var out []load.Generated
	for _, g := range kept {
		if !affected[filepath.Dir(g.Target)] {
			out = append(out, g)
		}
	}
	for _, g := range fresh {
		if affected[filepath.Dir(g.Target)] {
			out = append(out, g)
		}
	}
	slices.SortFunc(out, func(a, b load.Generated) int { return strings.Compare(a.Target, b.Target) })
	return out
}

// ormBases is a watch's last answer for the module's slot of each module
// root, with the state it was given.
var ormBases sync.Map // slot → *ormBase

type ormBase struct {
	state   *ormState
	missing []load.Generated
}
