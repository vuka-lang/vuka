package check

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// Templates checks ui.Template's Render calls: the number of values against
// the template's holes (E0127), and each value's type against its hole's
// place where the type is known (E0128) — what fails the render otherwise.
var Templates = &analysis.Analyzer{
	Name:      "templates",
	Doc:       "check ui.Template Render calls against the template's holes",
	URL:       docsURL,
	Run:       runTemplates,
	FactTypes: []analysis.Fact{new(holesFact)},
}

// holesFact is a package-level template's holes, for the packages
// rendering it.
type holesFact struct {
	N     int
	Slots []slot
}

func (*holesFact) AFact()           {}
func (f *holesFact) String() string { return fmt.Sprintf("%d holes", f.N) }

func runTemplates(pass *analysis.Pass) (any, error) {
	info := pass.TypesInfo
	parsed := func(e ast.Expr) *holesFact {
		call, ok := ast.Unparen(e).(*ast.CallExpr)
		if !ok || len(call.Args) != 1 || funcOf(info, call, uiPath) != "Template" {
			return nil
		}
		src, ok := str(info, call.Args[0])
		if !ok {
			return nil
		}
		n, slots, ok := holes(src)
		if !ok {
			return nil
		}
		return &holesFact{n, slots}
	}
	vars := map[types.Object]*holesFact{}
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.ValueSpec:
				for i, id := range x.Names {
					if i < len(x.Values) {
						if h := parsed(x.Values[i]); h != nil {
							vars[info.Defs[id]] = h
						}
					}
				}
			case *ast.AssignStmt:
				if x.Tok == token.DEFINE && len(x.Lhs) == len(x.Rhs) {
					for i, l := range x.Lhs {
						if id, ok := l.(*ast.Ident); ok && info.Defs[id] != nil {
							if h := parsed(x.Rhs[i]); h != nil {
								vars[info.Defs[id]] = h
							}
						}
					}
				}
			}
			return true
		})
	}
	for obj, h := range vars {
		if obj.Parent() == pass.Pkg.Scope() {
			pass.ExportObjectFact(obj, h)
		}
	}
	k := newKinds(pass)
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || call.Ellipsis.IsValid() {
				return true
			}
			m, recv := method(info, call, uiPath, "Markup")
			if m != "Render" {
				return true
			}
			h := parsed(recv)
			if h == nil {
				var id *ast.Ident
				switch r := ast.Unparen(recv).(type) {
				case *ast.Ident:
					id = r
				case *ast.SelectorExpr:
					id = r.Sel
				}
				if obj := info.Uses[id]; id != nil && obj != nil {
					if h = vars[obj]; h == nil {
						var fact holesFact
						if pass.ImportObjectFact(obj, &fact) {
							h = &fact
						}
					}
				}
			}
			if h == nil {
				return true
			}
			if len(call.Args) != h.N {
				report(pass, call, "E0127", "give Render one value per hole, {{0}} first", "%s has %d holes, and Render is given %d values", types.ExprString(recv), h.N, len(call.Args))
				return true
			}
			for _, s := range h.Slots {
				arg := call.Args[s.Hole]
				if why := k.why(s, info.TypeOf(arg)); why != "" {
					report(pass, arg, "E0128", "", "{{%d}} is %s; it is given %s, %s", s.Hole, s.Where, typeString(pass, info.TypeOf(arg)), why)
				}
			}
			return true
		})
	}
	return nil, nil
}

// kinds tells the types a hole's place takes, as Markup.Render does.
type kinds struct {
	node, stateful, anyState, jsChain, stringer *types.Interface
}

func newKinds(pass *analysis.Pass) *kinds {
	str := types.NewFunc(token.NoPos, nil, "String", types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(types.NewVar(token.NoPos, nil, "", types.Typ[types.String])), false))
	return &kinds{
		node:     iface(pass, uiPath, "Node"),
		stateful: iface(pass, uiPath, "Stateful"),
		anyState: iface(pass, uiPath, "AnyState"),
		jsChain:  iface(pass, uiPath, "JSChain"),
		stringer: types.NewInterfaceType([]*types.Func{str}, nil).Complete(),
	}
}

const (
	attrsWhy   = "an attribute set: write it where an attribute goes, <div {{n}}>"
	handlerWhy = "an event handler: only an on… attribute takes one"
	textWhy    = "not text: give the whole value to one hole"
)

// why is why a value of type t can't fill slot s, "" when it can or the
// type doesn't say (an interface).
func (k *kinds) why(s slot, t types.Type) string {
	if t == nil {
		return ""
	}
	if b, ok := t.(*types.Basic); ok && b.Kind() == types.UntypedNil {
		return ""
	}
	t = types.Default(t)
	if types.IsInterface(t) {
		return ""
	}
	basic := func(ks ...types.BasicKind) bool {
		for _, kd := range ks {
			if types.Identical(t, types.Typ[kd]) {
				return true
			}
		}
		return false
	}
	ui := func(names ...string) bool {
		for _, n := range names {
			if is(t, uiPath, n) {
				return true
			}
		}
		return false
	}
	impl := func(is ...*types.Interface) bool {
		for _, i := range is {
			if implements(t, i) {
				return true
			}
		}
		return false
	}
	sliceOf := func(pkg, name string) bool {
		sl, ok := t.(*types.Slice)
		return ok && is(sl.Elem(), pkg, name)
	}
	attrs := ui("Attr") || sliceOf(uiPath, "Attr") || is(t, templPath, "Attributes")
	_, isFunc := t.Underlying().(*types.Signature)
	scalar := basic(types.String, types.Int, types.Int64, types.Float64, types.Bool)
	switch s.Kind {
	case kChild:
		switch {
		case scalar || impl(errorIface) || ui("Binding"):
		case attrs:
			return attrsWhy
		case ui("EventHandler") || impl(k.jsChain):
			return handlerWhy
		case ui("Class", "Style"):
			return "an attribute's value"
		case impl(k.node, k.stateful, k.anyState) || named(t, uiPath, "ComponentNode"):
		case isFunc:
			return "a func: an element's on… attribute takes it"
		}
	case kText:
		switch {
		case scalar || impl(errorIface) || ui("TextNode"):
		case impl(k.node, k.stateful, k.anyState) || attrs || ui("EventHandler", "Class", "Style") || impl(k.jsChain) || isFunc:
			return "not text"
		}
	case kValue:
		if s.Event {
			if ui("EventHandler") || is(t, templPath, "ComponentScript") || impl(k.jsChain) || isFunc {
				return ""
			}
			return "not a handler: a func, ui.On(…), JS commands or a templ.ComponentScript"
		}
		switch {
		case scalar || ui("Binding"):
		case attrs:
			return attrsWhy
		case ui("EventHandler") || impl(k.jsChain):
			return handlerWhy
		case ui("Class"):
			if !s.Class {
				return "a class: only class takes one"
			}
		case ui("Style"):
			if !s.Style {
				return "a style: only style takes one"
			}
		case impl(k.stateful) || named(t, uiPath, "ComponentNode"):
			return "a component: a child hole takes it"
		case impl(k.anyState):
		case isFunc:
			return "a func: only an on… attribute takes one"
		}
	case kPart:
		mapSB := types.NewMap(types.Typ[types.String], types.Typ[types.Bool])
		switch {
		case basic(types.String, types.Int, types.Int64, types.Float64) || is(t, templPath, "SafeURL") || impl(errorIface, k.stringer):
		case ui("Class") || types.Identical(t, types.NewSlice(types.Typ[types.String])) || types.Identical(t, mapSB):
			if !s.Class {
				return "a class: only class takes one"
			}
		case impl(k.node):
			return textWhy
		default:
			if b, ok := t.Underlying().(*types.Basic); ok && b.Info()&(types.IsString|types.IsInteger|types.IsFloat) != 0 {
				return ""
			}
			return textWhy
		}
	case kSet:
		if !attrs {
			return "not an attribute set: a ui.Attr, []ui.Attr or templ.Attributes"
		}
	}
	return ""
}
