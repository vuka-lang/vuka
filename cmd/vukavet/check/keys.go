package check

import (
	"go/ast"
	"go/types"
	"regexp"

	"golang.org/x/tools/go/analysis"
)

// firstTag is a template's first element and its attributes.
var firstTag = regexp.MustCompile(`^\s*<([A-Za-z][\w.:-]*)([^>]*)>`)

var keyAttr = regexp.MustCompile(`(^|\s)key=`)

// listKeys warns where a loop builds a list without keys: in a live
// component's markup, items whose first element has no key (W0102), and
// anywhere, a stateful component with none (W0103) — the live render tells
// their instances apart by order, so an insertion moves every one after it.
func listKeys(pass *analysis.Pass) {
	info := pass.TypesInfo
	stateful := lookup(pass.Pkg, uiPath, "Stateful")
	var iface *types.Interface
	if tn, ok := stateful.(*types.TypeName); ok {
		iface, _ = tn.Type().Underlying().(*types.Interface)
	}
	isStateful := func(e ast.Expr) bool {
		t := info.TypeOf(e)
		return iface != nil && t != nil && types.Implements(t, iface)
	}
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			live := fd.Recv != nil && embedsLive(info.TypeOf(fd.Recv.List[0].Type))
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				var body *ast.BlockStmt
				switch l := n.(type) {
				case *ast.RangeStmt:
					body = l.Body
				case *ast.ForStmt:
					body = l.Body
				default:
					return true
				}
				ast.Inspect(body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "append" && len(call.Args) > 1 {
						for _, a := range call.Args[1:] {
							if isStateful(a) {
								report(pass, a, "W0103", "give it a key: <Counter key={it.ID} …/>", "a stateful component in a loop has no key; its instances are told apart by order")
							}
							inner, ok := ast.Unparen(a).(*ast.CallExpr)
							if !ok || funcOf(info, inner, uiPath) != "HTML" || len(inner.Args) == 0 {
								continue
							}
							src, ok := str(info, inner.Args[0])
							m := firstTag.FindStringSubmatch(src)
							if live && ok && m != nil && !isComponentTag(m[1]) && !keyAttr.MatchString(m[2]) {
								report(pass, inner, "W0102", "give the item's first element a key: <"+m[1]+" key={it.ID}>", "a live list's items have no key; a patch matches them by position")
							}
						}
					}
					if funcOf(info, call, uiPath) == "HTML" {
						src := firstArg(info, call)
						labels, slots, ok := holes(src)
						// an item keyed by its first element keys what it holds
						if m := firstTag.FindStringSubmatch(src); ok && (m == nil || !keyAttr.MatchString(m[2])) {
							for _, s := range slots {
								if s.Kind == kChild && s.Label+1 < len(call.Args) && isStateful(call.Args[s.Label+1]) {
									report(pass, call.Args[s.Label+1], "W0103", "give it a key: <Counter key={it.ID} …/>", "{%s}, a stateful component in a loop, has no key; its instances are told apart by order", labels[s.Label])
								}
							}
						}
					}
					return true
				})
				return false
			})
		}
	}
}

func firstArg(info *types.Info, call *ast.CallExpr) string {
	if len(call.Args) == 0 {
		return ""
	}
	s, _ := str(info, call.Args[0])
	return s
}

func isComponentTag(tag string) bool {
	return tag != "" && (tag[0] >= 'A' && tag[0] <= 'Z' || regexp.MustCompile(`\.`).MatchString(tag))
}

// embedsLive reports whether t, through a pointer, is a struct embedding
// ui.Live: a live component.
func embedsLive(t types.Type) bool {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := 0; i < st.NumFields(); i++ {
		if f := st.Field(i); f.Embedded() && named(f.Type(), uiPath, "Live") {
			return true
		}
	}
	return false
}
