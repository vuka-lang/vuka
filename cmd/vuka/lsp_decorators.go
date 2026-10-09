package main

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// projectDecorator is a decorator declared somewhere in the module.
type projectDecorator struct {
	name, pkgName, importPath, dir string
	kind                           string // "decorator", "decorator factory", "type decorator"
}

var (
	pkgClause    = regexp.MustCompile(`(?m)^package\s+(\w+)`)
	decoKeyword  = regexp.MustCompile(`(?m)^decorator\s+([A-Za-z_]\w*)\s*\(`)
	callFunc     = regexp.MustCompile(`(?m)^func\s+([A-Za-z_]\w*)\s*\(\s*\w+\s+\*\w+\.Call\s*\)\s*\{`)
	factoryFunc  = regexp.MustCompile(`(?m)^func\s+([A-Za-z_]\w*)\s*\([^)]*\)\s*\w+\.Decorator\s*\{`)
	typeDecoFunc = regexp.MustCompile(`(?m)^func\s+([A-Za-z_]\w*)\s*\(\s*\w+\s+\*\w+\.Type\s*\)\s*\{`)
)

// findDecorators lists the decorators declared in the module at root, by a
// quick read of its .vuka and .go files: decorator name(c), and functions of
// the shape func(c *vuka.Call), func(…) vuka.Decorator or func(t *vuka.Type).
func findDecorators(root, modPath string, read func(string) ([]byte, error)) []projectDecorator {
	var out []projectDecorator
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
				name == "testdata" || name == "vendor" || name == "node_modules" || fileExists(filepath.Join(path, ".vuka-build")) ||
				fileExists(filepath.Join(path, "go.mod"))) {
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
		m := pkgClause.FindSubmatch(src)
		if m == nil {
			return nil
		}
		dir := filepath.Dir(path)
		rel, _ := filepath.Rel(root, dir)
		imp := modPath
		if rel != "." {
			imp += "/" + filepath.ToSlash(rel)
		}
		add := func(re *regexp.Regexp, kind string) {
			for _, g := range re.FindAllSubmatch(src, -1) {
				out = append(out, projectDecorator{name: string(g[1]), pkgName: string(m[1]), importPath: imp, dir: dir, kind: kind})
			}
		}
		if strings.HasSuffix(name, ".vuka") {
			add(decoKeyword, "decorator")
		}
		add(callFunc, "decorator")
		add(factoryFunc, "decorator factory")
		add(typeDecoFunc, "type decorator")
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
			name = s.path[strings.LastIndex(s.path, "/")+1:]
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
		if d.kind == "decorator factory" {
			detail = "vuka.Decorator"
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
			at, text := addImports(f.text(), []importSpec{{path: d.importPath}})
			pos := positionOf(f.text(), at)
			item["additionalTextEdits"] = []any{map[string]any{"range": lspRange{pos, pos}, "newText": text}}
		}
		items = append(items, item)
	}
	return items
}

func isExported(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}
