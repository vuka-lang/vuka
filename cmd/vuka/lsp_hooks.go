package main

// Completion of a method's name on an orm model: the ORM's hooks
// (ormHooks), with the signatures the orm the module requires declares
// (ormHookSigs, as vet's W0503 reads them), as snippets. Hooks the model
// defines already aren't offered.

import (
	"go/ast"
	"go/parser"
	"go/types"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// hookSpot is a method name being written: func (p Post) Bef|.
type hookSpot struct {
	typeName string
	typed    lspRange // the name typed so far, replaced by a completion
}

// methodHead matches a method declaration's line up to its name.
var methodHead = regexp.MustCompile(`^func\s*\(\s*(?:[A-Za-z_]\w*\s+)?\*?\s*([A-Za-z_]\w*)(?:\[[^\]]*\])?\s*\)\s*([A-Za-z_]\w*)?$`)

// hookSpotAt is the spot when the cursor at off writes a method's name.
func hookSpotAt(src []byte, off int) *hookSpot {
	start := strings.LastIndexByte(string(src[:off]), '\n') + 1
	m := methodHead.FindSubmatchIndex(src[start:off])
	if m == nil {
		return nil
	}
	nameStart := off
	if m[4] >= 0 {
		nameStart = start + m[4]
	}
	return &hookSpot{typeName: string(src[start+m[2] : start+m[3]]), typed: lspRange{positionOf(src, nameStart), positionOf(src, off)}}
}

// hookItems are the completion items for spot when its type is one of the
// package's orm models: the hooks it doesn't define, each a snippet of its
// signature, with an edit adding the imports the signature needs.
func (p *proxy) hookItems(vf *vfile, spot *hookSpot) []any {
	src := vf.text()
	srcs := [][]byte{src}
	for _, f := range p.packageSources(vf.source) {
		if !strings.HasSuffix(f.Name, ".go") && filepath.Join(filepath.Dir(vf.source), f.Name) != vf.source {
			srcs = append(srcs, f.Src)
		}
	}
	model := false
	defined := map[string]bool{}
	for _, s := range srcs {
		if as := importedAs(s, ormModule); as != "" {
			for _, lt := range structTypes(s) {
				model = model || lt.name == spot.typeName && lt.orm == as
			}
		}
		for _, m := range modelMethod.FindAllSubmatch(s, -1) {
			if string(m[2]) == spot.typeName {
				defined[string(m[3])] = true
			}
		}
	}
	if !model {
		return nil
	}
	p.mu.Lock()
	root := p.modRoot
	p.mu.Unlock()
	sigs := ormHookSigs(root)
	byPath := map[string]string{} // path → the name the file imports it by
	for _, spec := range importList(src) {
		name := spec.name
		if name == "" {
			name = transpile.ImportName(spec.path)
		}
		byPath[spec.path] = name
	}
	var items []any
	for _, name := range slices.Sorted(maps.Keys(sigs)) {
		if defined[name] {
			continue
		}
		want := sigs[name]
		var missing []importSpec
		sig, ok := signatureWithNames(want.text, func(q string) string {
			path := ormModule
			if q != "" {
				path = want.imports[q]
			}
			if as, ok := byPath[path]; ok {
				return as
			}
			if !slices.ContainsFunc(missing, func(s importSpec) bool { return s.path == path }) {
				missing = append(missing, importSpec{path: path})
			}
			return transpile.ImportName(path)
		})
		if !ok {
			continue
		}
		item := map[string]any{
			"label":            name,
			"kind":             2.0,
			"detail":           name + sig,
			"sortText":         "0" + name,
			"filterText":       name,
			"insertTextFormat": 2.0,
			"textEdit":         map[string]any{"range": spot.typed, "newText": name + sig + " {\n\t$0\n}"},
		}
		if doc := firstSentence(want.doc); doc != "" {
			item["documentation"] = map[string]any{"kind": "markdown", "value": doc}
		}
		if len(missing) > 0 {
			item["additionalTextEdits"] = importEditsJSON(src, sourceImportEdits(vf, missing))
		}
		items = append(items, item)
	}
	return items
}

// signatureWithNames is a signature's text, (ctx context.Context) error,
// its packages named by pkg as signatureText names them, its parameters'
// names kept.
func signatureWithNames(sig string, pkg func(q string) string) (string, bool) {
	e, err := parser.ParseExpr("func" + sig)
	ft, ok := e.(*ast.FuncType)
	if err != nil || !ok {
		return "", false
	}
	for _, fl := range []*ast.FieldList{ft.Params, ft.Results} {
		if fl != nil {
			for _, f := range fl.List {
				qualify(f.Type, pkg)
			}
		}
	}
	return strings.TrimPrefix(types.ExprString(ft), "func"), true
}

// firstSentence is a doc comment's first sentence, on one line.
func firstSentence(doc string) string {
	doc = strings.Join(strings.Fields(doc), " ")
	if i := strings.Index(doc, ". "); i >= 0 {
		return doc[:i+1]
	}
	return doc
}
