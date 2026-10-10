package transpile

import (
	"go/build"
	"go/constant"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// JSXVersion is the version of the JSX target contract this compiler lowers
// to. A package is a JSX target when it declares `const VukaJSX = 1`:
//
//	required  Node, Attr{Name string; Value any}, El(tag, []Attr, ...Node),
//	          Text(any), Child(any), Fragment(...Node), Nodes(func(func(Node))),
//	          Try(Node, error)
//	optional  On(handler) — on… attributes of elements are events
//	          Stateful + Component(site, key, props) — tags naming struct types
//	          WithChildren(Node, children Node) — children for components that
//	          read them from their context
//
// A .vuka file renders its JSX with the one target it imports.
const JSXVersion = 1

// jsxTarget is the package a file's JSX lowers to.
type jsxTarget struct {
	path     string
	q        string // the qualifier, "ui." ("" for a dot import)
	pkg      *types.Package
	implicit bool // the runtime, for want of an imported target
}

// has reports whether the target declares name.
func (t *jsxTarget) has(name string) bool {
	if t.implicit {
		return true
	}
	return t.pkg != nil && t.pkg.Scope().Lookup(name) != nil
}

type importSpec struct {
	name, path string
	off        int
}

// tokImports reads a file's imports from its tokens, before it can be parsed.
func tokImports(toks []tok) []importSpec {
	var out []importSpec
	for i := 0; i < len(toks); i++ {
		switch toks[i].tok {
		case token.FUNC, token.TYPE, token.VAR, token.CONST:
			return out
		case token.IMPORT:
		default:
			continue
		}
		spec := func(j int) int {
			name := ""
			if j < len(toks) && (toks[j].tok == token.IDENT || toks[j].tok == token.PERIOD) {
				name = toks[j].lit
				if toks[j].tok == token.PERIOD {
					name = "."
				}
				j++
			}
			if j < len(toks) && toks[j].tok == token.STRING {
				if p, err := strconv.Unquote(toks[j].lit); err == nil {
					out = append(out, importSpec{name, p, toks[j].off})
				}
			}
			return j
		}
		i++
		if i < len(toks) && toks[i].tok == token.LPAREN {
			for i++; i < len(toks) && toks[i].tok != token.RPAREN; i++ {
				if toks[i].tok != token.SEMICOLON && toks[i].tok != token.COMMENT {
					i = spec(i)
				}
			}
		} else {
			i = spec(i)
		}
	}
	return out
}

// stdPackage reports whether an import path is a standard library package, which
// is never a JSX target.
func stdPackage(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	if strings.Contains(first, ".") || path == "C" {
		return path == "C"
	}
	_, err := os.Stat(filepath.Join(build.Default.GOROOT, "src", filepath.FromSlash(path)))
	return err == nil
}

// importTarget is the package at path when it is a JSX target, imported once
// per package.
func (e *engine) importTarget(path string) *types.Package {
	if p, ok := e.targets[path]; ok {
		return p
	}
	var pkg *types.Package
	if !stdPackage(path) {
		if p, err := e.imp.Import(path); err == nil && p != nil {
			if _, ok := p.Scope().Lookup("VukaJSX").(*types.Const); ok {
				pkg = p
			}
		}
	}
	if e.targets == nil {
		e.targets = map[string]*types.Package{}
	}
	e.targets[path] = pkg
	return pkg
}

// jsxTarget decides, at the file's first JSX expression (off), the target it
// lowers to: the JSX target it imports.
func (f *fileState) jsxTarget(toks []tok, off int, errs *ErrorList) *jsxTarget {
	if f.target != nil {
		return f.target
	}
	var found []*jsxTarget
	for _, imp := range tokImports(toks) {
		pkg := f.eng.importTarget(imp.path)
		if pkg == nil {
			continue
		}
		t := &jsxTarget{path: imp.path, pkg: pkg}
		switch imp.name {
		case "", pkg.Name():
			t.q = pkg.Name() + "."
		case ".":
		case "_":
			errs.add(f.at(imp.off), "JSX renders with %s, which a blank import doesn't name: import it by name", imp.path)
		default:
			t.q = imp.name + "."
		}
		if v := pkg.Scope().Lookup("VukaJSX").(*types.Const).Val(); v.Kind() != constant.Int || constant.Compare(v, token.NEQ, constant.MakeInt64(JSXVersion)) {
			errs.add(f.at(imp.off), "%s implements JSX contract %s; this vuka knows version %d: update vuka", imp.path, v, JSXVersion)
		}
		found = append(found, t)
	}
	switch len(found) {
	case 0:
		f.target = &jsxTarget{path: RuntimePath, q: f.scannedRuntime(toks) + ".", implicit: true}
	case 1:
		f.target = found[0]
		if f.target.path == RuntimePath {
			f.scannedRuntime(toks)
		}
	default:
		errs.add(f.at(off), "JSX in this file could render with %s or %s: a file imports one JSX target", found[0].path, found[1].path)
		f.target = found[0]
	}
	return f.target
}

// targetObj is a name the file's JSX target declares, as the package's type
// check sees it.
func (e *engine) targetObj(t *jsxTarget, name string) types.Object {
	if t == nil {
		return nil
	}
	if e.pkg != nil {
		for _, p := range e.pkg.Imports() {
			if p.Path() == t.path {
				return p.Scope().Lookup(name)
			}
		}
	}
	if t.pkg != nil {
		return t.pkg.Scope().Lookup(name)
	}
	return nil
}

// isTargetNamed reports whether t is the target's type name.
func isTargetNamed(tg *jsxTarget, t types.Type, name string) bool {
	return tg != nil && isNamed(t, tg.path, name)
}
