package check

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Variants checks what ui.Variants are given, as their Class method does
// when a component renders: a name that isn't a variant and a value a
// variant doesn't have (E0371) — in the variable's Defaults, a constant
// passed to Class, or a tag's attribute setting a prop the component
// passes to Class (<Alert tone="eror">).
var Variants = &analysis.Analyzer{
	Name:      "variants",
	Doc:       "check the values given to ui.Variants and the props a component passes to them",
	URL:       docsURL,
	Run:       runVariants,
	FactTypes: []analysis.Fact{new(variantsFact), new(propVariantFact)},
}

// variantsFact is a ui.Variants variable's variants and their values.
type variantsFact struct{ Variants map[string][]string }

func (*variantsFact) AFact()         {}
func (*variantsFact) String() string { return "variants" }

// propVariantFact marks a component's field its Class call passes as the
// value of the variant Name.
type propVariantFact struct {
	Name   string
	Values []string
}

func (*propVariantFact) AFact()           {}
func (f *propVariantFact) String() string { return "variant(" + f.Name + ")" }

func runVariants(pass *analysis.Pass) (any, error) {
	info := pass.TypesInfo
	vars := map[*types.Var]map[string][]string{}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.VAR {
				continue
			}
			for _, sp := range g.Specs {
				vs := sp.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						break
					}
					lit, ok := ast.Unparen(vs.Values[i]).(*ast.CompositeLit)
					v, _ := info.Defs[name].(*types.Var)
					if !ok || v == nil || !is(info.TypeOf(lit), uiPath, "Variants") {
						continue
					}
					dims := variantValues(pass, lit)
					vars[v] = dims
					pass.ExportObjectFact(v, &variantsFact{dims})
				}
			}
		}
	}
	dimsOf := func(v *types.Var) map[string][]string {
		if d, ok := vars[v]; ok {
			return d
		}
		var f variantsFact
		if pass.ImportObjectFact(v, &f) {
			return f.Variants
		}
		return nil
	}
	props := map[*types.Var]*propVariantFact{}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || call.Ellipsis.IsValid() {
				return true
			}
			m, recv := method(info, call, uiPath, "Variants")
			if m != "Class" {
				return true
			}
			v := varOf(info, recv)
			dims := dimsOf(v)
			if dims == nil {
				return true
			}
			for i := 0; i+1 < len(call.Args); i += 2 {
				name, ok := constString(info, call.Args[i])
				if !ok || name == "class" {
					continue
				}
				values, known := dims[name]
				if !known {
					unknownVariant(pass, call.Args[i], v.Name(), name, dims)
					continue
				}
				if value, ok := constString(info, call.Args[i+1]); ok {
					if value != "" && !slices.Contains(values, value) {
						unknownValue(pass, call.Args[i+1], name, value, values)
					}
					continue
				}
				if fv := fieldOf(info, call.Args[i+1]); fv != nil && fv.Pkg() == pass.Pkg {
					props[fv] = &propVariantFact{Name: name, Values: values}
					pass.ExportObjectFact(fv, props[fv])
				}
			}
			return true
		})
	}
	propOf := func(fv *types.Var) *propVariantFact {
		if p, ok := props[fv]; ok {
			return p
		}
		var f propVariantFact
		if pass.ImportObjectFact(fv, &f) {
			return &f
		}
		return nil
	}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if _, ok := info.TypeOf(lit).Underlying().(*types.Struct); !ok {
				return true
			}
			for _, el := range lit.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, _ := kv.Key.(*ast.Ident)
				fv, _ := info.ObjectOf(key).(*types.Var)
				if fv == nil || !fv.IsField() {
					continue
				}
				p := propOf(fv)
				value, ok := constString(info, kv.Value)
				if p == nil || !ok || value == "" || slices.Contains(p.Values, value) {
					continue
				}
				owner := strings.TrimPrefix(types.TypeString(info.TypeOf(lit), types.RelativeTo(pass.Pkg)), "*")
				help := `give it one of its values: ` + quoted(p.Values)
				if s := nearest(value, p.Values); s != "" {
					help = "did you mean " + strconv.Quote(s) + "?"
				}
				report(pass, kv.Value, "E0371", help, "%s's %s is its variant %s, which has no value %q: it is %s",
					owner, fv.Name(), p.Name, value, quoted(p.Values))
			}
			return true
		})
	}
	return nil, nil
}

// variantValues reads a ui.Variants literal's variants and values, and
// reports a default naming a variant or value it doesn't have.
func variantValues(pass *analysis.Pass, lit *ast.CompositeLit) map[string][]string {
	info := pass.TypesInfo
	dims := map[string][]string{}
	var defaults *ast.CompositeLit
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, _ := kv.Key.(*ast.Ident)
		m, _ := ast.Unparen(kv.Value).(*ast.CompositeLit)
		if key == nil || m == nil {
			continue
		}
		switch key.Name {
		case "Variants":
			for _, e := range m.Elts {
				kv, ok := e.(*ast.KeyValueExpr)
				name, named := constString(info, kv.Key)
				values, isLit := ast.Unparen(kv.Value).(*ast.CompositeLit)
				if !ok || !named || !isLit {
					continue
				}
				dims[name] = []string{}
				for _, v := range values.Elts {
					if kv, ok := v.(*ast.KeyValueExpr); ok {
						if s, ok := constString(info, kv.Key); ok {
							dims[name] = append(dims[name], s)
						}
					}
				}
				slices.Sort(dims[name])
			}
		case "Defaults":
			defaults = m
		}
	}
	if defaults != nil {
		for _, e := range defaults.Elts {
			kv, ok := e.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			name, ok1 := constString(info, kv.Key)
			value, ok2 := constString(info, kv.Value)
			values, known := dims[name]
			switch {
			case !ok1 || !ok2:
			case !known:
				unknownVariant(pass, kv.Key, "", name, dims)
			case value != "" && !slices.Contains(values, value):
				unknownValue(pass, kv.Value, name, value, values)
			}
		}
	}
	return dims
}

func unknownVariant(pass *analysis.Pass, at ast.Expr, v, name string, dims map[string][]string) {
	var names []string
	for n := range dims {
		names = append(names, n)
	}
	slices.Sort(names)
	help := `name one of them, or "class" for the caller's classes`
	if s := nearest(name, names); s != "" {
		help = "did you mean " + strconv.Quote(s) + "?"
	}
	what := "the variants"
	if v != "" {
		what = v + "'s variants"
	}
	report(pass, at, "E0371", help, "%s have no %q: they are %s", what, name, quoted(names))
}

func unknownValue(pass *analysis.Pass, at ast.Expr, name, value string, values []string) {
	help := "give it one of its values, or add " + strconv.Quote(value) + " to its Variants"
	if s := nearest(value, values); s != "" {
		help = "did you mean " + strconv.Quote(s) + "?"
	}
	report(pass, at, "E0371", help, "the variant %s has no value %q: it is %s", name, value, quoted(values))
}

func quoted(ss []string) string {
	q := make([]string, len(ss))
	for i, s := range ss {
		q[i] = strconv.Quote(s)
	}
	if len(q) > 1 {
		return strings.Join(q[:len(q)-1], ", ") + " or " + q[len(q)-1]
	}
	return strings.Join(q, "")
}

func constString(info *types.Info, x ast.Expr) (string, bool) {
	tv, ok := info.Types[x]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}

// varOf is the package-level variable x names: alert, or pkg.Alert.
func varOf(info *types.Info, x ast.Expr) *types.Var {
	switch x := ast.Unparen(x).(type) {
	case *ast.Ident:
		v, _ := info.Uses[x].(*types.Var)
		return v
	case *ast.SelectorExpr:
		v, _ := info.Uses[x.Sel].(*types.Var)
		if v != nil && v.IsField() {
			return nil
		}
		return v
	}
	return nil
}

// fieldOf is the struct field x reads: a.Tone.
func fieldOf(info *types.Info, x ast.Expr) *types.Var {
	sel, ok := ast.Unparen(x).(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	if s, ok := info.Selections[sel]; ok && s.Kind() == types.FieldVal {
		v, _ := s.Obj().(*types.Var)
		return v
	}
	return nil
}
