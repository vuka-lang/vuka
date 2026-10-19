package transpile

import (
	"fmt"
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
// to. A package is a JSX target when it declares `const VukaJSX = 3`:
//
//	required  Node, HTML(markup string, values ...any) Node — each piece of
//	          JSX, its expressions in labelled holes; blocks build a Node or
//	          a []Node; Try(Node, error); El, Safe and Child — scripts and
//	          styles with values; Fragment(...Node) and Child(any) —
//	          placeholders while types are unknown
//	optional  On — on… attributes of elements are events
//	          Stateful + Key(key, Stateful) — tags naming struct types
//	          WithChildren(Node, children Node) — children for components that
//	          read them from their context
//	          UseState, UsePageState and State's methods — client state
//	          Ref(name string) — ref="name" attributes name elements, and each
//	          Ref("name") in a function with JSX must name one of its elements
//
// A .vuka file renders its JSX with the one target it imports.
const JSXVersion = 3

// DefaultJSXTarget is the JSX target messages suggest.
const DefaultJSXTarget = "github.com/vuka-lang/ui"

// jsxTarget is the package a file's JSX lowers to. It is usually a package the
// file imports (q = "ui."), but a file whose OWN package declares VukaJSX
// renders its JSX with unqualified calls of that package (self, q = ""): the
// ui library authors its own components. A self-target's pkg isn't known until
// the package is type-checked, so names holds the package-level names its JSX
// asks about before then (scanned from the source), and resolveSelfTargets
// fills path and pkg once check has run.
type jsxTarget struct {
	path       string
	q          string // the qualifier, "ui." ("" for a dot import or a self-target)
	pkg        *types.Package
	self       bool            // the file's own package is the target
	names      map[string]bool // a self-target's package-level names, before it is type-checked
	verChecked bool            // its VukaJSX version has been checked
}

// has reports whether the target declares name. Before a self-target is
// type-checked its pkg is nil, so the names scanned from the source answer.
func (t *jsxTarget) has(name string) bool {
	if t == nil {
		return false
	}
	if t.pkg != nil {
		return t.pkg.Scope().Lookup(name) != nil
	}
	return t.names[name]
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
		p, err := e.imp.Import(path)
		switch {
		case err != nil:
			if e.targetErrs == nil {
				e.targetErrs = map[string]error{}
			}
			e.targetErrs[path] = err
		case p != nil:
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
			errs.add("E0104", f.at(imp.off), "JSX renders with %s, which a blank import doesn't name: import it by name", imp.path)
		default:
			t.q = imp.name + "."
		}
		if v := pkg.Scope().Lookup("VukaJSX").(*types.Const).Val(); v.Kind() != constant.Int || constant.Compare(v, token.NEQ, constant.MakeInt64(JSXVersion)) {
			errs.add("E0104", f.at(imp.off), "%s implements JSX contract %s; this vuka knows version %d: update %s", imp.path, v, JSXVersion, olderOf(v, imp.path))
		}
		found = append(found, t)
	}
	switch len(found) {
	case 0:
		if f.eng.selfJSX {
			// The file's own package declares VukaJSX: its JSX renders with
			// unqualified calls of the package itself. resolveSelfTargets sets
			// path and pkg once the package is type-checked.
			f.target = &jsxTarget{self: true, names: f.eng.selfNames}
			break
		}
		why := ""
		for _, imp := range tokImports(toks) {
			if err := f.eng.targetErrs[imp.path]; err != nil {
				why = fmt.Sprintf(" (%s doesn't load: %s)", imp.path, oneLine(err.Error()))
				break
			}
		}
		errs.add("E0102", f.at(off), "JSX needs a target: import %s (or another package declaring VukaJSX)%s", DefaultJSXTarget, why)
		f.target = &jsxTarget{q: "__jsx."}
	case 1:
		f.target = found[0]
	default:
		errs.add("E0103", f.at(off), "JSX in this file could render with %s or %s: a file imports one JSX target", found[0].path, found[1].path)
		f.target = found[0]
	}
	return f.target
}

// detectSelfTarget scans the package's sources for a top-level VukaJSX
// declaration — which makes the package its own JSX target — and the
// package-level names a self-target's JSX asks about before the package is
// type-checked (its primitives, e.g. F, FT and On). It runs once, before the
// files are scanned, so jsxTarget can decide a self-target without types.
func (e *engine) detectSelfTarget(files []File) {
	e.selfNames = map[string]bool{}
	for _, file := range files {
		toks := scanTokens(file.Src)
		depth := 0
		for i := 0; i < len(toks); i++ {
			t := toks[i]
			switch {
			case isOpen(t.tok):
				depth++
				continue
			case isClose(t.tok):
				depth--
				continue
			}
			// VukaJSX, wherever it is declared (a lone const or a group), marks
			// the package as its own target; a selector x.VukaJSX does not.
			if t.tok == token.IDENT && t.lit == "VukaJSX" && (i == 0 || toks[i-1].tok != token.PERIOD) {
				e.selfJSX = true
			}
			if depth != 0 {
				continue
			}
			if t.tok == token.FUNC || t.tok == token.TYPE {
				j := i + 1
				for j < len(toks) && (toks[j].tok == token.COMMENT || toks[j].tok == token.SEMICOLON) {
					j++
				}
				if j < len(toks) && toks[j].tok == token.IDENT {
					e.selfNames[toks[j].lit] = true
				}
			}
		}
	}
}

// resolveSelfTargets points each self-target at the package now that it is
// type-checked, so its component tags resolve against the package's own scope
// and has/targetObj read the real declarations. e.pkg is rebuilt every round,
// so the target's pkg is refreshed to keep its types from going stale. The
// VukaJSX version is checked once, as an imported target's is.
func (e *engine) resolveSelfTargets() {
	if e.pkg == nil {
		return
	}
	for _, f := range e.vuka {
		t := f.target
		if t == nil || !t.self {
			continue
		}
		t.path, t.pkg = e.pkg.Path(), e.pkg
		if t.verChecked {
			continue
		}
		t.verChecked = true
		off := 0
		if len(f.jsx) > 0 {
			off = f.jsx[0].start
		}
		c, ok := e.pkg.Scope().Lookup("VukaJSX").(*types.Const)
		switch {
		case !ok:
			e.errs.add("E0102", f.at(off), "JSX needs a target: import %s (or another package declaring VukaJSX)", DefaultJSXTarget)
		default:
			if v := c.Val(); v.Kind() != constant.Int || constant.Compare(v, token.NEQ, constant.MakeInt64(JSXVersion)) {
				e.errs.add("E0104", f.at(off), "this package implements JSX contract %s; this vuka knows version %d: update %s", v, JSXVersion, olderOf(v, "this package"))
			}
		}
	}
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

// declaredBy reports whether t is a named type the target declares.
func declaredBy(tg *jsxTarget, t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := types.Unalias(t).(*types.Named)
	return ok && tg != nil && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == tg.path
}

// provider is the target's exported struct whose pointer has method m: what a
// type embeds to get an unexported method of the target's interface.
func provider(tg *jsxTarget, m *types.Func) string {
	if tg.pkg == nil {
		return ""
	}
	sc := tg.pkg.Scope()
	for _, name := range sc.Names() {
		tn, ok := sc.Lookup(name).(*types.TypeName)
		if !ok || !tn.Exported() || tn.IsAlias() {
			continue
		}
		if _, ok := tn.Type().Underlying().(*types.Struct); !ok {
			continue
		}
		if o, _, _ := types.LookupFieldOrMethod(types.NewPointer(tn.Type()), true, m.Pkg(), m.Name()); o != nil {
			return tg.q + name
		}
	}
	return ""
}

// oneLine is a multi-line error (the go command's) on one line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// HasJSX reports whether Vuka source holds a JSX expression, found by tokens
// as the transpiler finds them: a < that starts an operand and is followed by
// a tag name or >.
func HasJSX(src []byte) bool {
	prev := token.SEMICOLON
	for _, t := range scanTokens(src) {
		if t.tok == token.COMMENT {
			continue
		}
		if t.tok == token.LSS && jsxStarts(prev) && jsxTagAt(src, t.off) {
			return true
		}
		prev = t.tok
	}
	return false
}

// olderOf is what to update when a target implements contract v: the
// target when it is older than this vuka, else vuka.
func olderOf(v constant.Value, target string) string {
	if v.Kind() == constant.Int && constant.Compare(v, token.LSS, constant.MakeInt64(JSXVersion)) {
		return target
	}
	return "vuka"
}
