package main

import (
	"hash/maphash"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// projectDecorator is a decorator declared somewhere in the module.
type projectDecorator struct {
	name, pkgName, importPath, dir string
	params                         bool   // a composed decorator's: written with arguments
	kind                           string // "decorator", "decorator factory", "type decorator", "composed decorator"
}

var (
	pkgClause    = regexp.MustCompile(`(?m)^package\s+(\w+)`)
	decoKeyword  = regexp.MustCompile(`(?m)^decorator\s+([A-Za-z_]\w*)\s*\(\s*\w+\s*\)\s*\{`)
	decoFactory  = regexp.MustCompile(`(?m)^decorator\s+([A-Za-z_]\w*)\s*\([^()]*\)\s*\(\s*\w+\s*\)\s*\{`)
	composedDeco = regexp.MustCompile(`(?m)^decorator\s+([A-Za-z_]\w*)\s*(\(\s*\w[^)]*\))?\s*=`)
	bundleFunc   = regexp.MustCompile(`(?m)^func\s+([A-Za-z_]\w*)\s*\(([^)]*)\)\s*\w+\.Bundle\s*\{`)
	callFunc     = regexp.MustCompile(`(?m)^func\s+([A-Za-z_]\w*)\s*\(\s*\w+\s+\*\w+\.Call\s*\)\s*\{`)
	factoryFunc  = regexp.MustCompile(`(?m)^func\s+([A-Za-z_]\w*)\s*\([^)]*\)\s*\w+\.Decorator\s*\{`)
	typeDecoFunc = regexp.MustCompile(`(?m)^func\s+([A-Za-z_]\w*)\s*\(\s*\w+\s+\*\w+\.Type\s*\)\s*\{`)
)

// findDecorators lists the decorators declared in the module at root, by a
// quick read of its .vuka and .go files: decorator name(c), and functions of
// the shape func(c *vuka.Call), func(…) vuka.Decorator or func(t *vuka.Type).
// A file's decorators are read again only when its text changed.
func findDecorators(root, modPath string, read func(string) ([]byte, error)) []projectDecorator {
	var out []projectDecorator
	_ = load.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
				name == "testdata" || name == "vendor" || name == "node_modules" || hasEntry(path, load.Marker) || load.HasGoMod(path)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !(strings.HasSuffix(name, ".vuka") || strings.HasSuffix(name, ".go")) || strings.HasSuffix(name, "_test.go") ||
			strings.HasSuffix(name, "_test.vuka") || strings.HasSuffix(name, "_vuka.go") {
			return nil
		}
		src, err := read(path)
		if err != nil {
			return nil
		}
		found := fileDecorators(path, src)
		if len(found) == 0 {
			return nil
		}
		dir := filepath.Dir(path)
		rel, _ := filepath.Rel(root, dir)
		imp := modPath
		if rel != "." {
			imp += "/" + filepath.ToSlash(rel)
		}
		for _, d := range found {
			d.importPath, d.dir = imp, dir
			out = append(out, d)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].importPath != out[j].importPath {
			return out[i].importPath < out[j].importPath
		}
		return out[i].name < out[j].name
	})
	return out
}

// decoratorItems are completion items for the module's decorators in other
// packages, qualified by package and importing it when f doesn't.
func decoratorItems(f *vfile, decos []projectDecorator, typed lspRange, qualifier string) []any {
	imported := map[string]string{} // import path → name used in f
	for _, s := range importList(f.text()) {
		name := s.name
		if name == "" {
			name = transpile.ImportName(s.path)
		}
		imported[s.path] = name
	}
	here := filepath.Dir(f.source)
	var items []any
	for _, d := range decos {
		samePkg := d.dir == here
		if !samePkg && !isExported(d.name) {
			continue
		}
		local, ok := imported[d.importPath]
		if !ok {
			local = d.pkgName
		}
		label := local + "." + d.name
		switch {
		case samePkg && qualifier != "":
			continue
		case samePkg:
			label, ok = d.name, true // the package's own: no qualifier, no import
		case qualifier != "" && qualifier != local:
			continue
		}
		detail := "func(c *vuka.Call)"
		switch {
		case d.kind == "decorator factory":
			detail = "vuka.Decorator"
		case d.kind == "composed decorator" && d.params:
			detail = "func(…) vuka.Bundle"
		case d.kind == "composed decorator":
			detail = "func() vuka.Bundle"
		}
		insert, format := attrInsert(label, detail)
		item := map[string]any{
			"label":            label,
			"kind":             3,
			"detail":           d.kind + " · " + d.importPath,
			"sortText":         "0" + label,
			"filterText":       label,
			"textEdit":         map[string]any{"range": typed, "newText": insert},
			"insertTextFormat": format,
		}
		if !ok {
			item["additionalTextEdits"] = importEditsJSON(f.text(), sourceImportEdits(f, []importSpec{{path: d.importPath}}))
		}
		items = append(items, item)
	}
	return items
}

func isExported(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

// decoScans are the decorators each file declares, as of a text of it
// (fileDecorators).
var decoScans struct {
	sync.Mutex
	seed  maphash.Seed
	files map[string]decoScan
}

type decoScan struct {
	sum   uint64 // the text's maphash
	decos []projectDecorator
}

// fileDecorators are the decorators the file at path, with the text src,
// declares, without their import path and directory: read again only when
// the text changed since the last time.
func fileDecorators(path string, src []byte) []projectDecorator {
	decoScans.Lock()
	if decoScans.files == nil {
		decoScans.seed, decoScans.files = maphash.MakeSeed(), map[string]decoScan{}
	}
	sum := maphash.Bytes(decoScans.seed, src)
	if c, ok := decoScans.files[path]; ok && c.sum == sum {
		decoScans.Unlock()
		return c.decos
	}
	decoScans.Unlock()
	decos := scanDecorators(filepath.Base(path), src)
	decoScans.Lock()
	decoScans.files[path] = decoScan{sum, decos}
	decoScans.Unlock()
	return decos
}

// scanDecorators reads the decorators a file named name, with the text src,
// declares.
func scanDecorators(name string, src []byte) []projectDecorator {
	m := pkgClause.FindSubmatch(src)
	if m == nil {
		return nil
	}
	pkg := string(m[1])
	var out []projectDecorator
	add := func(re *regexp.Regexp, kind string) {
		for _, g := range re.FindAllSubmatch(src, -1) {
			out = append(out, projectDecorator{name: string(g[1]), pkgName: pkg, kind: kind})
		}
	}
	if strings.HasSuffix(name, ".vuka") {
		add(decoKeyword, "decorator")
		add(decoFactory, "decorator factory")
		for _, g := range composedDeco.FindAllSubmatch(src, -1) {
			out = append(out, projectDecorator{name: string(g[1]), pkgName: pkg, kind: "composed decorator", params: len(g[2]) > 0})
		}
	}
	for _, g := range bundleFunc.FindAllSubmatch(src, -1) {
		out = append(out, projectDecorator{name: string(g[1]), pkgName: pkg, kind: "composed decorator", params: len(strings.TrimSpace(string(g[2]))) > 0})
	}
	add(callFunc, "decorator")
	add(factoryFunc, "decorator factory")
	add(typeDecoFunc, "type decorator")
	return out
}

// hasEntry reports whether the directory dir has an entry named name, from
// its listing as load.ReadDir keeps it.
func hasEntry(dir, name string) bool {
	entries, _ := load.ReadDir(dir)
	for _, e := range entries {
		if e.Name() == name {
			return true
		}
	}
	return false
}
