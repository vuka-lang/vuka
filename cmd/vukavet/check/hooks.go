package check

import (
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Hooks checks the methods of orm models named like the ORM's hooks: one
// whose signature isn't the hook's is ignored by the ORM (W0503). The
// signatures are the orm package's own, its hook interfaces'; the receiver,
// value or pointer, never matters: the ORM calls them on a *T.
var Hooks = &analysis.Analyzer{
	Name: "ormhooks",
	Doc:  "check that methods of orm models named like the ORM's hooks have the hooks' signatures",
	URL:  docsURL,
	Run:  runHooks,
}

// ormHooks are the orm's interfaces a model's methods are found by:
// what it declares (indexes, checks, generated columns) and its lifecycle.
var ormHooks = []string{
	"IndexesDeclarer", "ChecksDeclarer", "GeneratedDeclarer",
	"BeforeCreater", "AfterCreater", "BeforeSaver", "AfterSaver", "BeforeDeleter", "AfterDeleter", "RowValidator",
}

func runHooks(pass *analysis.Pass) (any, error) {
	orm := imported(pass.Pkg, ormPath)
	if orm == nil {
		return nil, nil
	}
	hooks := hookMethods(orm)
	for _, name := range pass.Pkg.Scope().Names() {
		tn, ok := pass.Pkg.Scope().Lookup(name).(*types.TypeName)
		if !ok || tn.IsAlias() {
			continue
		}
		n, ok := tn.Type().(*types.Named)
		if !ok || !embedsModel(n, map[types.Type]bool{}) {
			continue
		}
		for m := range n.Methods() {
			want, ok := hooks[m.Name()]
			if !ok {
				continue
			}
			got := m.Type().(*types.Signature)
			if types.Identical(got, want) {
				continue
			}
			recv := typeString(pass, got.Recv().Type())
			sig, must := funcString(pass, recv, m.Name(), got), funcString(pass, recv, m.Name(), want)
			report(pass, at(pass, m, nil), "W0503", "declare it as "+must+", or name it otherwise",
				"%s.%s looks like an ORM hook, but its signature is %s; it must be %s: the ORM ignores it otherwise", n.Obj().Name(), m.Name(), sig, must)
		}
	}
	return nil, nil
}

// hookMethods are the hooks' methods by name, from orm's interfaces.
func hookMethods(orm *types.Package) map[string]*types.Signature {
	out := map[string]*types.Signature{}
	for _, name := range ormHooks {
		tn, ok := orm.Scope().Lookup(name).(*types.TypeName)
		if !ok {
			continue // an orm without it
		}
		if it, ok := tn.Type().Underlying().(*types.Interface); ok && it.NumMethods() == 1 {
			out[it.Method(0).Name()] = it.Method(0).Type().(*types.Signature)
		}
	}
	return out
}

// embedsModel reports whether struct type t embeds orm.Model or orm.Base,
// directly or through the structs it embeds by value.
func embedsModel(t types.Type, seen map[types.Type]bool) bool {
	s, ok := t.Underlying().(*types.Struct)
	if !ok || seen[t] {
		return false
	}
	seen[t] = true
	for f := range s.Fields() {
		if !f.Embedded() {
			continue
		}
		if named(f.Type(), ormPath, "Model") || named(f.Type(), ormPath, "Base") {
			return true
		}
		if _, ptr := f.Type().(*types.Pointer); !ptr && embedsModel(f.Type(), seen) {
			return true
		}
	}
	return false
}

// imported is the package path pkg imports, directly or not; nil when it
// doesn't.
func imported(pkg *types.Package, path string) *types.Package {
	seen := map[*types.Package]bool{}
	var find func(*types.Package) *types.Package
	find = func(p *types.Package) *types.Package {
		for _, im := range p.Imports() {
			if im.Path() == path {
				return im
			}
			if !seen[im] {
				seen[im] = true
				if found := find(im); found != nil {
					return found
				}
			}
		}
		return nil
	}
	return find(pkg)
}

// funcString is a method's declaration as the package analysed writes it,
// its parameters' types alone: func (*User) BeforeSave(context.Context) error.
func funcString(pass *analysis.Pass, recv, name string, sig *types.Signature) string {
	unnamed := func(t *types.Tuple) *types.Tuple {
		var vs []*types.Var
		for v := range t.Variables() {
			vs = append(vs, types.NewParam(v.Pos(), v.Pkg(), "", v.Type()))
		}
		return types.NewTuple(vs...)
	}
	plain := types.NewSignatureType(nil, nil, nil, unnamed(sig.Params()), unnamed(sig.Results()), sig.Variadic())
	return "func (" + recv + ") " + name + strings.TrimPrefix(types.TypeString(plain, qualifier(pass)), "func")
}
