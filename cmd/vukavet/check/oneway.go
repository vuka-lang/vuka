package check

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// A page is registered by the pages/ tree alone: vuka writes its
// registration in pages/route.go, a generated file. A route is a page when
// what it registers renders —
//
//   - Live(path, …): a live page;
//   - Page(path, …), or a route with ui.Island(…) among its options: an
//     island page;
//   - a constructor, func(deps…) *T or (*T, error), whose *T is a ui.Node
//     or whose T embeds ui.Live: a page's struct.
//
// A page is a struct that renders. Every other route is a function, a
// handler — returning data, nothing or an error, writing the response
// itself, or answering with a ui.Node it builds from components (a
// fragment a form post re-renders) — which a person registers with app.Get
// and the other verbs. Registering a page by hand, outside a generated file,
// is E0720; a _test.go file may (a test mounts the page it tests). The
// options only pages/route.go writes are W0702 by hand. web.Routes
// registering a handler stays: it is what a route decorator on a function
// lowers to, and what vuka eject leaves.

// generatedOnly are web's names only vuka's generated registration calls.
var generatedOnly = map[string]bool{"PageRoute": true, "LayoutOf": true, "GuardOf": true, "ReloadLayout": true, "UseOf": true, "Receiver": true}

// fileOf is the file of pass holding pos.
func fileOf(pass *analysis.Pass, pos token.Pos) *ast.File {
	for _, f := range pass.Files {
		if f.FileStart <= pos && pos <= f.FileEnd {
			return f
		}
	}
	return nil
}

// byHand reports whether pos is in a file a person writes: not generated,
// and, unless tests count, not a _test.go file.
func byHand(pass *analysis.Pass, pos token.Pos, tests bool) bool {
	f := fileOf(pass, pos)
	if f == nil || ast.IsGenerated(f) {
		return false
	}
	return tests || !strings.HasSuffix(pass.Fset.Position(pos).Filename, "_test.go")
}

// checkPages reports pages registered by hand (E0720), and hand-written
// uses of the options pages/route.go alone calls (W0702).
func checkPages(pass *analysis.Pass, regs []registration) {
	node, live := uiTypes(pass.Pkg)
	for _, r := range regs {
		if r.verb == "Controller" || r.verb == "Service" || !byHand(pass, r.at.pos, false) {
			continue
		}
		what := pageWhy(pass, r, node, live)
		if what == "" {
			continue
		}
		where := "app." + r.verb
		if r.global {
			where = "web.Routes." + r.verb
		}
		report(pass, r.at, "E0720", "put the page under pages/: its file's path is its URL, and vuka registers it; an API stays app.Get",
			"%s(%q, %s) registers a page (%s) by hand: a page is a file under pages/", where, r.path, types.ExprString(r.fn), what)
	}
	for _, f := range pass.Files {
		if ast.IsGenerated(f) || strings.HasSuffix(pass.Fset.Position(f.Pos()).Filename, "_test.go") {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if name := funcOf(pass.TypesInfo, c, webPath); generatedOnly[name] {
				report(pass, c.Fun, "W0702", "put the page under pages/: a _layout.vuka.html, a guard.go or a middleware.go in its folder applies to it",
					"web.%s is what pages/route.go registers with; it is not an API to write by hand", name)
			}
			return true
		})
	}
}

// pageWhy says what makes r a page, "" when it is an API.
func pageWhy(pass *analysis.Pass, r registration, node *types.Interface, live types.Type) string {
	if r.verb == "Live" {
		return "a live page"
	}
	if r.verb == "Page" || r.island(pass.TypesInfo) {
		return "an island page"
	}
	t := pass.TypesInfo.TypeOf(r.fn)
	if t == nil || node == nil {
		return ""
	}
	sig, ok := t.Underlying().(*types.Signature)
	if !ok || sig.Results().Len() == 0 {
		return ""
	}
	if n, _ := ctor(sig, nil); n != nil {
		if types.Implements(types.NewPointer(n), node) || types.Implements(n, node) || embeds(n, live) {
			return "its struct " + n.Obj().Name() + " renders"
		}
		return ""
	}
	return ""
}

// embeds reports whether the struct n embeds live.
func embeds(n *types.Named, live types.Type) bool {
	st, ok := n.Underlying().(*types.Struct)
	if !ok || live == nil {
		return false
	}
	for i := range st.NumFields() {
		if f := st.Field(i); f.Embedded() && types.Identical(f.Type(), live) {
			return true
		}
	}
	return false
}

// uiTypes are ui.Node's interface and the type ui.Live, from what pkg
// imports, directly or not; nil when it imports no ui.
func uiTypes(pkg *types.Package) (*types.Interface, types.Type) {
	seen := map[*types.Package]bool{}
	var find func(p *types.Package) *types.Package
	find = func(p *types.Package) *types.Package {
		if seen[p] {
			return nil
		}
		seen[p] = true
		if p.Path() == uiPath {
			return p
		}
		for _, imp := range p.Imports() {
			if u := find(imp); u != nil {
				return u
			}
		}
		return nil
	}
	ui := find(pkg)
	if ui == nil {
		return nil, nil
	}
	var node *types.Interface
	if obj, ok := ui.Scope().Lookup("Node").(*types.TypeName); ok {
		node, _ = obj.Type().Underlying().(*types.Interface)
	}
	var live types.Type
	if obj, ok := ui.Scope().Lookup("Live").(*types.TypeName); ok {
		live = obj.Type()
	}
	return node, live
}

// handWrittenHTML reports ui.HTML and ui.IslandPage called outside
// generated files (W0106): they are what the compiler writes for a
// .vuka.html file's markup, and for an island page's.
func handWrittenHTML(pass *analysis.Pass) {
	if strings.HasPrefix(pass.Pkg.Path(), uiPath) && (len(pass.Pkg.Path()) == len(uiPath) || pass.Pkg.Path()[len(uiPath)] == '/') {
		return // ui's own
	}
	for _, f := range pass.Files {
		if ast.IsGenerated(f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch funcOf(pass.TypesInfo, c, uiPath) {
			case "HTML":
				report(pass, c.Fun, "W0106", "write the markup in a .vuka.html file: the struct it renders above, the markup below, used as a tag",
					"ui.HTML is what the compiler writes for a .vuka.html file's markup; it is not an API to write by hand")
			case "IslandPage":
				report(pass, c.Fun, "W0106", "write the page as a .vuka.html file whose markup is the island's tag, the component imported above it: import Orders \"./Orders.vue\" … <Orders client:load />",
					"ui.IslandPage is what the compiler writes for an island page, a .vuka.html file whose markup is its component's tag; it is not an API to write by hand")
			}
			return true
		})
	}
}
