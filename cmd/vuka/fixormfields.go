package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// fixORMFields migrates generated lookup values and their relation hops,
// keeping receiver types and user-defined names intact. It runs only with
// an ORM that provides the new generator API.
func fixORMFields(m *fixModule) ([]fixChange, error) {
	c := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", ormModule)
	c.Dir = m.root
	out, err := c.Output()
	if err != nil {
		return nil, nil
	}
	api, _ := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "vuka_term.go"))
	if !bytes.Contains(api, []byte("type ModelAnnotation[")) {
		return nil, nil
	}
	overlay := map[string][]byte{}
	for _, g := range m.gens {
		overlay[g.Target] = g.Src
	}
	pkgs, err := packages.Load(&packages.Config{Dir: m.root, Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps, Tests: true, Overlay: overlay}, "./...")
	if err != nil {
		return nil, err
	}
	methods := map[string]bool{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 {
					continue
				}
				t := fn.Recv.List[0].Type
				if p, ok := t.(*ast.StarExpr); ok {
					t = p.X
				}
				if id, ok := t.(*ast.Ident); ok && strings.HasSuffix(id.Name, "Fields") {
					methods[pkg.PkgPath+"."+strings.TrimSuffix(id.Name, "Fields")+"."+fn.Name.Name] = true
				}
			}
		}
	}
	changes := map[string][]byte{}
	for _, p := range pkgs {
		if p.TypesInfo == nil {
			continue
		}
		for _, f := range p.Syntax {
			path := p.Fset.Position(f.Pos()).Filename
			if !strings.HasPrefix(path, m.root+string(filepath.Separator)) {
				continue
			}
			src, err := os.ReadFile(path)
			if err != nil || bytes.Contains(src, []byte("Code generated")) {
				continue
			}
			if _, done := changes[path]; done {
				continue
			}
			rewritten, n := ormFieldsEdits(p, f, src, overlay, methods)
			if n > 0 {
				changes[path] = rewritten
			}
		}
	}
	var paths []string
	for path := range changes {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var result []fixChange
	for _, path := range paths {
		src := changes[path]
		result = append(result, fixChange{fmt.Sprintf("%s: ORM lookup values, relation methods and model annotation types", relPath(m.root, path)), func() error { return os.WriteFile(path, src, 0644) }})
	}
	if len(result) > 0 {
		result = append(result, fixChange{"regenerate model field lookups", func() error {
			tmp, err := os.MkdirTemp("", "vuka-orm-fields-")
			if err != nil {
				return err
			}
			defer os.RemoveAll(tmp)
			overlay, err := addToOverlay("", tmp, m.gens)
			if err != nil {
				return err
			}
			mod, err := os.ReadFile(filepath.Join(m.root, "go.mod"))
			if err != nil {
				return err
			}
			name, args := ormCommand(m.root, mod)
			args = append(args, "generate")
			if overlay != "" {
				args = append(args, "--overlay", overlay)
			}
			args = append(args, ".")
			cmd := exec.Command(name, args...)
			cmd.Dir = m.root
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("orm generate: %w\n%s", err, out)
			}
			return nil
		}})
	}
	return result, nil
}

type ormFieldModel struct {
	pkg  *types.Package
	name string
	t    types.Type
}

func (m *ormFieldModel) relation(name string) *ormFieldModel {
	obj, _, _ := types.LookupFieldOrMethod(m.t, false, m.pkg, name)
	if obj == nil {
		return nil
	}
	t := obj.Type()
	if sig, ok := t.(*types.Signature); ok {
		if sig.Results().Len() != 1 {
			return nil
		}
		t = sig.Results().At(0).Type()
	}
	return ormPathModel(t)
}
func ormPathModel(t types.Type) *ormFieldModel {
	if t == nil {
		return nil
	}
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return nil
	}
	name := n.Obj().Name()
	base, ok := strings.CutSuffix(name, "Fields")
	if !ok {
		base, ok = strings.CutSuffix(name, "Path")
	}
	if !ok {
		return nil
	}
	return ormModel(n.Obj().Pkg(), base)
}
func ormModel(pkg *types.Package, name string) *ormFieldModel {
	obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
	if !ok || !isModel(obj.Type()) {
		return nil
	}
	for _, suffix := range []string{"Columns", "Fields", "Path"} {
		if root := pkg.Scope().Lookup(name + suffix); root != nil {
			return &ormFieldModel{pkg, name, root.Type()}
		}
	}
	return nil
}

func ormFieldsEdits(p *packages.Package, f *ast.File, src []byte, overlay map[string][]byte, methods map[string]bool) ([]byte, int) {
	type edit struct {
		at, end int
		text    string
	}
	edits := map[int]edit{}
	offset := func(pos token.Pos) int { return p.Fset.Position(pos).Offset }
	add := func(pos, end token.Pos, text string) { at := offset(pos); edits[at] = edit{at, offset(end), text} }
	typeNodes := map[ast.Node]bool{}
	mark := func(n ast.Node) {
		if n != nil {
			ast.Inspect(n, func(n ast.Node) bool {
				if n != nil {
					typeNodes[n] = true
				}
				return true
			})
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Field:
			mark(n.Type)
		case *ast.TypeSpec:
			mark(n.Type)
		case *ast.ValueSpec:
			mark(n.Type)
		case *ast.CompositeLit:
			mark(n.Type)
		case *ast.TypeAssertExpr:
			mark(n.Type)
		case *ast.CallExpr:
			if id, ok := n.Fun.(*ast.Ident); ok && (id.Name == "new" || id.Name == "make") && len(n.Args) > 0 {
				mark(n.Args[0])
			}
		case *ast.IndexListExpr:
			for _, x := range n.Indices {
				mark(x)
			}
		case *ast.IndexExpr:
			if _, ok := p.TypesInfo.TypeOf(n.X).(*types.Signature); ok {
				mark(n.Index)
			}
		}
		return true
	})
	generated := func(obj types.Object) bool {
		if obj == nil || !obj.Pos().IsValid() {
			return false
		}
		path := p.Fset.Position(obj.Pos()).Filename
		text, ok := overlay[path]
		if !ok {
			text, _ = os.ReadFile(path)
		}
		return bytes.Contains(text, []byte("Code generated by ormgen"))
	}
	aliases := map[string]*ormFieldModel{}
	var resolve func(ast.Expr) *ormFieldModel
	resolve = func(e ast.Expr) *ormFieldModel {
		if e == nil {
			return nil
		}
		switch x := e.(type) {
		case *ast.Ident:
			if m := aliases[x.Name]; m != nil {
				return m
			}
			obj := p.TypesInfo.Uses[x]
			if obj != nil && obj.Pkg() != nil && obj.Parent() == obj.Pkg().Scope() {
				for _, suffix := range []string{"Fields", "Columns"} {
					if base, ok := strings.CutSuffix(obj.Name(), suffix); ok {
						return ormModel(obj.Pkg(), base)
					}
				}
			}
		case *ast.SelectorExpr:
			if obj := p.TypesInfo.Uses[x.Sel]; obj != nil && obj.Pkg() != nil {
				for _, suffix := range []string{"Fields", "Columns"} {
					if base, ok := strings.CutSuffix(obj.Name(), suffix); ok {
						return ormModel(obj.Pkg(), base)
					}
				}
			}
			if m := resolve(x.X); m != nil {
				return m.relation(x.Sel.Name)
			}
		case *ast.CallExpr:
			return resolve(x.Fun)
		case *ast.ParenExpr:
			return resolve(x.X)
		}
		return ormPathModel(p.TypesInfo.TypeOf(e))
	}
	var stack []ast.Node
	saved := map[ast.Node]map[string]*ormFieldModel{}
	clone := func() map[string]*ormFieldModel {
		out := map[string]*ormFieldModel{}
		for k, v := range aliases {
			out[k] = v
		}
		return out
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			last := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if old, ok := saved[last]; ok {
				aliases = old
			}
			return true
		}
		var parent ast.Node
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		stack = append(stack, n)
		switch n := n.(type) {
		case *ast.BlockStmt:
			saved[n] = clone()
		case *ast.FuncDecl:
			saved[n] = clone()
			if n.Type.Params != nil {
				for _, field := range n.Type.Params.List {
					for _, id := range field.Names {
						delete(aliases, id.Name)
					}
				}
			}
			if n.Recv != nil {
				for _, field := range n.Recv.List {
					for _, id := range field.Names {
						delete(aliases, id.Name)
					}
				}
			}
		case *ast.FuncLit:
			saved[n] = clone()
			if n.Type.Params != nil {
				for _, field := range n.Type.Params.List {
					for _, id := range field.Names {
						delete(aliases, id.Name)
					}
				}
			}
		case *ast.AssignStmt:
			for i, left := range n.Lhs {
				if id, ok := left.(*ast.Ident); ok {
					var m *ormFieldModel
					if i < len(n.Rhs) {
						m = resolve(n.Rhs[i])
					}
					if m == nil {
						delete(aliases, id.Name)
					} else {
						aliases[id.Name] = m
					}
				}
			}
		case *ast.ValueSpec:
			for i, id := range n.Names {
				var m *ormFieldModel
				if i < len(n.Values) {
					m = resolve(n.Values[i])
				}
				if m != nil {
					aliases[id.Name] = m
				} else {
					delete(aliases, id.Name)
				}
			}
		case *ast.SelectorExpr:
			if typeNodes[n] && n.Sel.Name == "Annotation" {
				if pkg, ok := p.TypesInfo.Uses[func() *ast.Ident { id, _ := n.X.(*ast.Ident); return id }()].(*types.PkgName); ok && pkg.Imported().Path() == ormModule {
					add(n.Sel.Pos(), n.Sel.End(), "ModelAnnotation")
				}
			}
			if !typeNodes[n] {
				if m := resolve(n.X); m != nil && m.relation(n.Sel.Name) != nil {
					if call, ok := parent.(*ast.CallExpr); !ok || call.Fun != n {
						add(n.End(), n.End(), "()")
					}
				}
			}
		case *ast.Ident:
			if typeNodes[n] || p.TypesInfo.Defs[n] != nil || !strings.HasSuffix(n.Name, "Fields") {
				break
			}
			obj := p.TypesInfo.Uses[n]
			if obj == nil || obj.Pkg() == nil || obj.Parent() != obj.Pkg().Scope() || !generated(obj) {
				break
			}
			base := strings.TrimSuffix(n.Name, "Fields")
			m := ormModel(obj.Pkg(), base)
			if m == nil {
				break
			}
			// Receiver method expressions remain type expressions, even while
			// a stale generated file still declares the old lookup variable.
			rootNode := ast.Node(n)
			use := parent
			if sel, ok := parent.(*ast.SelectorExpr); ok && sel.Sel == n {
				rootNode = sel
				if len(stack) >= 3 {
					use = stack[len(stack)-3]
				}
			}
			if sel, ok := use.(*ast.SelectorExpr); ok && sel.X == rootNode && methods[obj.Pkg().Path()+"."+base+"."+sel.Sel.Name] {
				break
			}
			// A receiver's method expression is already using the new type.
			if _, isType := obj.(*types.TypeName); isType {
				root := ast.Node(n)
				if sel, ok := parent.(*ast.SelectorExpr); ok && sel.Sel == n {
					root = sel
				}
				if len(stack) >= 2 {
					var use ast.Node = parent
					if root != n && len(stack) >= 3 {
						use = stack[len(stack)-3]
					}
					if sel, ok := use.(*ast.SelectorExpr); ok && sel.X == root {
						member, _, _ := types.LookupFieldOrMethod(m.t, false, m.pkg, sel.Sel.Name)
						if _, ok := member.(*types.Func); ok && m.relation(sel.Sel.Name) == nil {
							break
						}
					}
				}
			}
			add(n.Pos(), n.End(), base+"Columns")
		}
		return true
	})
	var ordered []edit
	for _, e := range edits {
		ordered = append(ordered, e)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].at > ordered[j].at })
	for _, e := range ordered {
		src = append(append(append([]byte(nil), src[:e.at]...), []byte(e.text)...), src[e.end:]...)
	}
	return src, len(ordered)
}
