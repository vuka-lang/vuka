package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// Component tags of packages a file doesn't import yet: after < the
// module's component packages (and the ui kit's) are offered by name,
// inserting design. and asking for completion again; after <design. the
// package's components are, each item adding the import as it is
// accepted.

// uiKitModule is the module whose packages are offered besides the app's.
const uiKitModule = "github.com/vuka-lang/ui/v2"

// compPkg is a package that declares components.
type compPkg struct {
	transpile.ImportablePackage
	comps []compEntry
}

// compEntry is a component a package declares.
type compEntry struct {
	name, kind, detail, doc string
}

// compDirs caches the components of each directory read, by the
// directory's files as they were (compStamp).
var compDirs sync.Map // dir → *compDir

type compDir struct {
	stamp string
	comps []compEntry
}

// componentPackages are the packages the file at srcPath can take
// component tags from: the module's, then the ui kit's; its own and those
// of a pages tree (routes) left out.
func componentPackages(srcPath string) []compPkg {
	dir := filepath.Dir(srcPath)
	root, modPath, err := load.ModuleRoot(dir)
	if err != nil {
		return nil
	}
	scope := transpile.ImportScope{ModuleRoot: root, ModulePath: modPath}
	self := ownImportPath(scope, dir)
	uiDir, uiName := "", ""
	if c, ok, _ := readUIConfig(root); ok {
		uiDir, uiName = filepath.Join(root, filepath.FromSlash(c.Dir)), c.Package
	}
	var out []compPkg
	for _, pk := range scope.Packages(uiKitModule) {
		if pk.Path == self || pk.Own && underPages(root, pk.Dir) {
			continue
		}
		if uiDir != "" && pk.Dir == uiDir {
			pk.Name = uiName
		}
		if comps := dirComponents(pk.Dir); len(comps) > 0 {
			out = append(out, compPkg{pk, comps})
		}
	}
	slices.SortStableFunc(out, func(a, b compPkg) int {
		if a.Own != b.Own {
			if a.Own {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Path, b.Path)
	})
	return out
}

// underPages reports whether dir is a folder route's: in the module's
// pages tree.
func underPages(root, dir string) bool {
	rel, err := filepath.Rel(root, dir)
	return err == nil && slices.Contains(strings.Split(filepath.ToSlash(rel), "/"), "pages")
}

// importSpecFor is the import of a package: named when its name isn't
// its path's last element.
func importSpecFor(pk transpile.ImportablePackage) importSpec {
	s := importSpec{path: pk.Path}
	if pk.Name != transpile.ImportName(pk.Path) {
		s.name = pk.Name
	}
	return s
}

var (
	compStructRe = regexp.MustCompile(`(?m)^type\s+([A-Z]\w*)(?:\[[^\]\n]*\])?\s+struct\s*\{`)
	compMethodRe = regexp.MustCompile(`(?m)^func\s*\(\s*\w*\s*\*?\s*([A-Z]\w*)(?:\[[^\]\n]*\])?\s*\)\s*(?:View|Render)\s*\(`)
	compFuncRe   = regexp.MustCompile(`(?m)^func\s+([A-Z]\w*)\s*(?:\[[^\]\n]*\])?\s*\([^\n{]*\)\s*\(?\s*(?:\w+\.)?(?:Node|Component)\b`)
)

// dirComponents are the components the package in dir declares: the
// structs its .vuka.html files render, the structs with a View or Render
// method or embedding Live, and the functions returning a Node.
func dirComponents(dir string) []compEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var stamp strings.Builder
	for _, e := range entries {
		if info, err := e.Info(); err == nil && !e.IsDir() {
			fmt.Fprintf(&stamp, "%s %d %d\n", e.Name(), info.Size(), info.ModTime().UnixNano())
		}
	}
	if v, ok := compDirs.Load(dir); ok && v.(*compDir).stamp == stamp.String() {
		return v.(*compDir).comps
	}
	var names []string
	add := func(n string) {
		if !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	structs := map[string]bool{}
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || strings.HasSuffix(n, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		switch {
		case strings.HasSuffix(n, ".vuka.html"):
			if sh, _, _ := shapeBounds(src); sh != nil && isExported(sh.Type) {
				add(sh.Type)
			}
		case strings.HasSuffix(n, ".vuka"), strings.HasSuffix(n, ".go") && !isGenerated(src):
			for _, m := range compStructRe.FindAllSubmatchIndex(src, -1) {
				structs[string(src[m[2]:m[3]])] = true
				if e := skipGo(src, m[1]-1); e > 0 && embedsLive.Match(src[m[1]:e]) {
					add(string(src[m[2]:m[3]]))
				}
			}
			for _, m := range compMethodRe.FindAllSubmatch(src, -1) {
				add(string(m[1]))
			}
			for _, m := range compFuncRe.FindAllSubmatch(src, -1) {
				add(string(m[1]))
			}
		}
	}
	var comps []compEntry
	for _, n := range names {
		info := tagComponentIn(dir, n)
		if info == nil {
			continue
		}
		comps = append(comps, compEntry{name: n, kind: info.kind, detail: propsDetail(info), doc: typeDoc(info.file, info.line)})
	}
	slices.SortFunc(comps, func(a, b compEntry) int { return strings.Compare(a.name, b.name) })
	compDirs.Store(dir, &compDir{stamp.String(), comps})
	return comps
}

// propsDetail is a component's first props, as a completion's detail.
func propsDetail(info *tagInfo) string {
	var ps []string
	for _, pr := range info.props {
		if len(ps) == 3 {
			ps = append(ps, "…")
			break
		}
		ps = append(ps, pr.attr+" "+pr.typ)
	}
	if len(ps) == 0 {
		return info.kind
	}
	return info.kind + " · " + strings.Join(ps, ", ")
}

// typeDoc is the comment above line (from 1) of file.
func typeDoc(file string, line int) string {
	src, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	return docAbove(src, lineStart(src, uint32(max(line-1, 0))))
}

// unimportedTagItems are the completion items of component packages the
// file doesn't import for a tag being typed: the packages after <, a
// package's components after <pkg. when pkg is none of the file's
// imports.
func unimportedTagItems(vf *vfile, spot jsxSpot, typed lspRange) []any {
	src := vf.text()
	names, paths := map[string]bool{}, map[string]bool{}
	for _, im := range sourceImports(vf.source, src) {
		n := im.Name
		if n == "" {
			n = transpile.ImportName(im.Path)
		}
		names[n], paths[im.Path] = true, true
	}
	q := strings.TrimSuffix(spot.qual, ".")
	if q != "" && (names[q] || strings.Contains(q, ".")) {
		return nil
	}
	var items []any
	for _, pk := range componentPackages(vf.source) {
		if paths[pk.Path] || names[pk.Name] {
			continue
		}
		rank := "1"
		if pk.Own {
			rank = "0"
		}
		if q == "" {
			items = append(items, map[string]any{
				"label": pk.Name, "kind": 9, "detail": pk.Path, "sortText": "3" + rank + pk.Name, "filterText": pk.Name,
				"labelDetails":     map[string]any{"description": pk.Path},
				"textEdit":         map[string]any{"range": typed, "newText": pk.Name + "."},
				"insertTextFormat": 1,
				"command":          map[string]any{"title": "components", "command": "editor.action.triggerSuggest"},
			})
			continue
		}
		if pk.Name != q {
			continue
		}
		edits := importEditsJSON(src, sourceImportEdits(vf, []importSpec{importSpecFor(pk.ImportablePackage)}))
		for _, c := range pk.comps {
			kind := 7
			if c.kind == "function" {
				kind = 3
			}
			it := map[string]any{
				"label": c.name, "kind": kind, "detail": c.detail, "sortText": "0" + rank + c.name, "filterText": c.name,
				"labelDetails":        map[string]any{"description": pk.Path},
				"textEdit":            map[string]any{"range": typed, "newText": c.name},
				"insertTextFormat":    1,
				"additionalTextEdits": edits,
			}
			doc := "`import " + importSpecFor(pk.ImportablePackage).line() + "`"
			if c.doc != "" {
				doc = c.doc + "\n\n" + doc
			}
			it["documentation"] = map[string]any{"kind": "markdown", "value": doc}
			items = append(items, it)
		}
	}
	return items
}
