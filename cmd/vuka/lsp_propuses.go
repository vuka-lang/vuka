package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// A prop's references in markup. Each tag naming a component sets its props
// as attributes, which the generated Go writes as the keys of a composite
// literal no source position maps to, so gopls's references of a props
// field never reach them. They are found from the component's references
// instead: each is a tag's name, and the tag's attribute that binds to the
// field is a reference of it.

// propRefs is the references of the props field declared at pos of the Go
// file uri (generated or not): gopls's, mapped onto the sources, and the
// attributes setting it on the component's tags.
func (p *proxy) propRefs(uri string, pos lspPosition, context any, origin *vfile) []any {
	var refs []any
	if res, ok := p.ask("textDocument/references", map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": pos, "context": context}); ok {
		var v any
		_ = json.Unmarshal(res, &v)
		refs, _ = p.rewrite(v, nil, origin).([]any)
	}
	return withLocations(refs, p.fieldTagRefs(uri, pos, origin)...)
}

// fieldTagRefs is the attributes setting the props field declared at pos
// of the Go file uri on the component's tags; nil when no struct field is
// declared there.
func (p *proxy) fieldTagRefs(uri string, pos lspPosition, origin *vfile) []any {
	src := p.goFile(uri)
	typePos, fields, index, ok := structField(src, offsetOf(src, pos))
	if !ok {
		return nil
	}
	return p.tagAttrRefs(uri, positionOf(src, typePos), fields, index, origin)
}

// tagAttrRefs is the attributes binding to fields[index] on the tags of
// the component declared at typePos of the Go file uri.
func (p *proxy) tagAttrRefs(uri string, typePos lspPosition, fields []string, index int, origin *vfile) []any {
	res, ok := p.ask("textDocument/references", map[string]any{"textDocument": map[string]any{"uri": uri},
		"position": typePos, "context": map[string]any{"includeDeclaration": false}})
	if !ok {
		return nil
	}
	var v any
	_ = json.Unmarshal(res, &v)
	uses, _ := p.rewrite(v, nil, origin).([]any)
	var out []any
	texts := map[string][]byte{}
	for _, u := range uses {
		loc, _ := u.(map[string]any)
		file, _ := loc["uri"].(string)
		r, ok := toRange(loc["range"])
		if !ok || !isVuka(uriToPath(file)) {
			continue
		}
		text, ok := texts[file]
		if !ok {
			text = p.sourceText(p.real(uriToPath(file)))
			texts[file] = text
		}
		off := offsetOf(text, r.Start)
		at := strings.LastIndexByte(string(text[:off]), '<')
		if at < 0 {
			continue
		}
		t, ok := readTag(text, at)
		if !ok || t.close || off < t.nameStart || off > t.nameEnd {
			continue
		}
		for _, a := range t.attrs {
			if bindName(fields, a.name) == index || a.name == lowerCamel(fields[index]) {
				out = append(out, map[string]any{"uri": file, "range": lspRange{positionOf(text, a.start), positionOf(text, a.end)}})
			}
		}
	}
	return out
}

// structField reads the Go src for the struct field named at off: where
// its type's name is, the struct's field names, and which is the one.
func structField(src []byte, off int) (typePos int, fields []string, index int, ok bool) {
	fset := token.NewFileSet()
	file, _ := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if file == nil {
		return 0, nil, 0, false
	}
	ast.Inspect(file, func(n ast.Node) bool {
		ts, isType := n.(*ast.TypeSpec)
		if ok || !isType {
			return !ok
		}
		st, isStruct := ts.Type.(*ast.StructType)
		if !isStruct {
			return false
		}
		var names []string
		at := -1
		for _, f := range st.Fields.List {
			for _, id := range f.Names {
				if fset.Position(id.Pos()).Offset == off {
					at = len(names)
				}
				names = append(names, id.Name)
			}
		}
		if at >= 0 {
			typePos, fields, index, ok = fset.Position(ts.Name.Pos()).Offset, names, at, true
		}
		return false
	})
	return typePos, fields, index, ok
}

// withLocations appends to list the locations of more it doesn't hold.
func withLocations(list []any, more ...any) []any {
	key := func(e any) string {
		m, _ := e.(map[string]any)
		u, _ := m["uri"].(string)
		r, _ := toRange(m["range"])
		return fmt.Sprint(u, r)
	}
	seen := map[string]bool{}
	for _, e := range list {
		seen[key(e)] = true
	}
	for _, e := range more {
		if k := key(e); !seen[k] {
			seen[k] = true
			list = append(list, e)
		}
	}
	return list
}

// propDeclAt is where the generated Go declares the prop of the shape vf
// named at off (its declaration or a use in the markup); nil when off
// names none.
func propDeclAt(vf *vfile, off int) *lspPosition {
	if !strings.HasSuffix(vf.source, ".vuka.html") {
		return nil
	}
	src := vf.text()
	s := scanShape(src)
	r, ok := s.refAt(off)
	if !ok || r.kind != refProp || s.fields[r.name] == nil {
		return nil
	}
	pos := vf.toGen(positionOf(src, s.fields[r.name].off))
	return &pos
}

// withRanges is a list of highlights without those that lost their range
// mapping it onto the source: gopls's highlights of generated Go no source
// spells.
func withRanges(v any) any {
	list, ok := v.([]any)
	if !ok {
		return v
	}
	out := list[:0:0]
	for _, e := range list {
		if m, ok := e.(map[string]any); ok && m["range"] != nil {
			out = append(out, e)
		}
	}
	return out
}
