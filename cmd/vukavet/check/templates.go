package check

import (
	"go/ast"
	"go/printer"
	"go/scanner"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Templates checks ui.HTML calls: the values against the labels (E0127),
// each value's Go against its label (E0132), and each value's type against
// its hole's place where the type is known (E0128) — what fails the render,
// or reads wrong, otherwise — and the package's .vuka.html files: a bare
// name both a field and a package-level name (W0101) — and the loops that
// build a live list without keys (W0102, W0103).
var Templates = &analysis.Analyzer{
	Name: "templates",
	Doc:  "check ui.HTML's values against its labelled holes",
	URL:  docsURL,
	Run:  runTemplates,
}

func runTemplates(pass *analysis.Pass) (any, error) {
	info := pass.TypesInfo
	k := newKinds(pass)
	bareNames(pass, htmlSources(pass))
	listKeys(pass)
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || call.Ellipsis.IsValid() || len(call.Args) == 0 || funcOf(info, call, uiPath) != "HTML" {
				return true
			}
			src, ok := str(info, call.Args[0])
			if !ok {
				return true
			}
			labels, slots, ok := holes(src)
			if !ok {
				return true
			}
			args := call.Args[1:]
			if len(args) != len(labels) {
				report(pass, call, "E0127", "give ui.HTML one value per label, in the order the labels first appear", "ui.HTML has %d values (%s), and it is given %d", len(labels), labelList(labels), len(args))
				return true
			}
			for i, a := range args {
				if !sameGo(pass, a, labels[i]) {
					report(pass, a, "E0132", "write the label as the value's Go, or the value as the label's", "value %d is %s; its label is {%s}", i+1, types.ExprString(a), labels[i])
				}
			}
			for _, s := range slots {
				arg := args[s.Label]
				if why := k.why(s, info.TypeOf(arg)); why != "" {
					report(pass, arg, "E0128", "", "{%s} is %s; it is given %s, %s", labels[s.Label], s.Where, typeString(pass, info.TypeOf(arg)), why)
				}
			}
			return true
		})
	}
	return nil, nil
}

func labelList(ls []string) string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = "{" + l + "}"
	}
	return strings.Join(out, ", ")
}

// sameGo reports whether x is written as label is: the same tokens.
func sameGo(pass *analysis.Pass, x ast.Expr, label string) bool {
	var b strings.Builder
	if printer.Fprint(&b, pass.Fset, x) != nil {
		return true
	}
	return slices.Equal(goTokens(b.String()), goTokens(label))
}

// goTokens are s's Go tokens as text, comments and the line breaks Go
// reads as semicolons left out.
func goTokens(s string) []string {
	var sc scanner.Scanner
	fs := token.NewFileSet()
	sc.Init(fs.AddFile("", -1, len(s)), []byte(s), func(token.Position, string) {}, 0)
	var out []string
	for {
		_, tok, lit := sc.Scan()
		if tok == token.EOF {
			return out
		}
		if tok == token.SEMICOLON && lit == "\n" || tok == token.COMMA {
			continue // a trailing comma before a line break reads the same
		}
		if lit == "" {
			lit = tok.String()
		}
		out = append(out, lit)
	}
}

// kinds tells the types a hole's place takes, as ui.HTML does.
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
	attrsWhy   = "an attribute set: write it where an attribute goes, <div {attrs...}>"
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
	case kToggle:
		if !basic(types.Bool) && !impl(k.anyState) {
			return "not a bool or a ui.State[bool]"
		}
	case kSet:
		if !attrs {
			return "not an attribute set: a ui.Attr, []ui.Attr or templ.Attributes"
		}
	}
	return ""
}
