package main

import (
	"bytes"
	"sort"
	"strings"
)

// The outline and folding of a shape (.vuka.html): its struct holds what the
// file declares for it, its methods and its markup, so the struct's line
// stays pinned (sticky scroll follows the outline) while the markup scrolls;
// the Go header folds as one.

func isShape(vf *vfile) bool { return vf != nil && strings.HasSuffix(vf.source, ".vuka.html") }

// markupRoots are the shape's top-level elements, each with its children,
// as document symbols.
func markupRoots(src []byte) []map[string]any {
	elems := scanMarkup(src).elems
	sort.SliceStable(elems, func(i, j int) bool { return elems[i].open.start < elems[j].open.start })
	type node struct {
		sym      map[string]any
		end      int
		children []any
	}
	var roots []map[string]any
	var stack []*node
	flush := func(n *node) {
		if len(n.children) > 0 {
			n.sym["children"] = n.children
		}
	}
	for _, e := range elems {
		end := e.open.end
		if e.closed {
			end = e.close.end
		}
		for len(stack) > 0 && stack[len(stack)-1].end <= e.open.start {
			flush(stack[len(stack)-1])
			stack = stack[:len(stack)-1]
		}
		sym := map[string]any{
			"name":           elementLabel(src, e.open),
			"kind":           8, // Field, as HTML outlines show elements
			"range":          lspRange{positionOf(src, e.open.start), positionOf(src, end)},
			"selectionRange": lspRange{positionOf(src, e.open.nameStart), positionOf(src, e.open.nameEnd)},
		}
		if e.open.name == "" {
			sym["name"] = "<>"
		}
		n := &node{sym: sym, end: end}
		if len(stack) == 0 {
			roots = append(roots, sym)
		} else {
			p := stack[len(stack)-1]
			p.children = append(p.children, sym)
		}
		stack = append(stack, n)
	}
	for i := len(stack) - 1; i >= 0; i-- {
		flush(stack[i])
	}
	return roots
}

// elementLabel is an element's name in the outline: its tag, then its static
// id and classes (main#app.page).
func elementLabel(src []byte, t jsxTag) string {
	id, classes := "", ""
	for _, a := range t.attrs {
		if a.expr || a.valStart == 0 || a.valEnd-a.valStart < 2 {
			continue
		}
		v := string(src[a.valStart+1 : a.valEnd-1])
		if strings.ContainsAny(v, "{}") {
			continue
		}
		switch a.name {
		case "id":
			id = "#" + v
		case "class", "className":
			for _, c := range strings.Fields(v) {
				classes += "." + c
			}
		}
	}
	return t.name + id + classes
}

// shapeSymbols is gopls's outline of a shape's Go, mapped onto it, made the
// outline of the shape: the methods its markup compiles to are dropped (as are
// symbols that map nowhere), and the struct they belong to spans the file to
// the end of its markup, holding the fields, the methods and helpers declared
// with it and the markup's elements.
func shapeSymbols(vf *vfile, v any) any {
	src := vf.text()
	list, _ := v.([]any)
	roots := markupRoots(src)
	if len(list) > 0 {
		if m, _ := list[0].(map[string]any); m != nil && m["location"] != nil {
			return shapeSymbolInfos(vf, list, roots)
		}
	}
	var syms []map[string]any
	shape := ""
	for _, s := range list {
		m, _ := s.(map[string]any)
		_, ok := toRange(m["range"])
		sel, _ := toRange(m["selectionRange"])
		if recv, gen := generatedMethod(src, m, sel); gen {
			shape = recv
			continue
		}
		if ok && m["selectionRange"] != nil {
			syms = append(syms, m)
		}
	}
	out := []any{}
	var shapeSym map[string]any
	at := -1
	for i, m := range syms {
		if shape != "" && m["name"] == shape && m["kind"] == float64(23) {
			shapeSym, at = m, i
		}
	}
	if shapeSym == nil || len(roots) == 0 {
		for _, m := range syms {
			out = append(out, m)
		}
		for _, r := range roots {
			if !coveredBy(r, syms) {
				out = append(out, r)
			}
		}
		return out
	}
	start, _ := toRange(shapeSym["range"])
	last, _ := toRange(roots[len(roots)-1]["range"])
	span := lspRange{start.Start, last.End}
	if start.Start.Line > 0 {
		// From the type keyword's line: the symbol starts at the name.
		span.Start = lspPosition{Line: start.Start.Line}
	}
	kids, _ := shapeSym["children"].([]any)
	for i, m := range syms {
		if i == at {
			continue
		}
		r, _ := toRange(m["range"])
		if r.Start.Line > start.End.Line && r.End.Line <= span.End.Line {
			kids = append(kids, m)
			continue
		}
		out = append(out, m)
	}
	for _, r := range roots {
		kids = append(kids, r)
	}
	shapeSym["children"] = kids
	shapeSym["range"] = span
	out = append(out, shapeSym)
	sort.SliceStable(out, func(i, j int) bool {
		a, _ := toRange(out[i].(map[string]any)["range"])
		b, _ := toRange(out[j].(map[string]any)["range"])
		return a.Start.Line < b.Start.Line
	})
	return out
}

// generatedMethod reports whether a symbol is a method the shape's markup
// compiles to (View, Render): its name isn't written where it maps to.
func generatedMethod(src []byte, m map[string]any, sel lspRange) (string, bool) {
	full, _ := m["name"].(string)
	close := strings.Index(full, ").")
	if !strings.HasPrefix(full, "(") || close < 0 {
		return "", false
	}
	recv, name := strings.TrimPrefix(full[1:close], "*"), full[close+2:]
	s, e := offsetOf(src, sel.Start), offsetOf(src, sel.End)
	if s < e && e <= len(src) && string(src[s:e]) == name {
		return "", false
	}
	return recv, true
}

// coveredBy reports whether a symbol's range lies in one of syms'.
func coveredBy(sym map[string]any, syms []map[string]any) bool {
	r, _ := toRange(sym["range"])
	for _, m := range syms {
		o, _ := toRange(m["range"])
		if o.Start.Line <= r.Start.Line && r.End.Line <= o.End.Line {
			return true
		}
	}
	return false
}

// shapeSymbolInfos is shapeSymbols for an editor taking a flat list: the
// generated methods and unmapped symbols dropped, the markup's top-level
// elements added.
func shapeSymbolInfos(vf *vfile, list []any, roots []map[string]any) []any {
	src := vf.text()
	out := []any{}
	uri := ""
	for _, s := range list {
		m, _ := s.(map[string]any)
		loc, _ := m["location"].(map[string]any)
		r, ok := toRange(loc["range"])
		if !ok {
			continue
		}
		if u, _ := loc["uri"].(string); u != "" {
			uri = u
		}
		if _, gen := generatedMethod(src, m, lspRange{r.Start, r.Start}); gen && r.Start.Line >= markupLine(src) {
			continue
		}
		out = append(out, m)
	}
	if uri == "" {
		return out
	}
	for _, r := range roots {
		out = append(out, map[string]any{"name": r["name"], "kind": r["kind"],
			"location": map[string]any{"uri": uri, "range": r["range"]}})
	}
	return out
}

// markupLine is the line a shape's markup starts on.
func markupLine(src []byte) uint32 {
	sp, ok := splitShape(src)
	if !ok {
		return ^uint32(0)
	}
	return positionOf(src, sp.markup).Line
}

// shapeSplit is where a shape's Go header and its markup are: the one place
// that finds the boundary, for the outline, folding and completion.
type shapeSplit struct {
	goStart, goEnd int // the Go: from the package clause to the end of its last line
	markup         int // the first top-level element's <
}

// splitShape finds a shape's header and markup, the boundary read by
// shapeBounds (lsp_shapescan.go); false when it has no markup or no package
// clause.
func splitShape(src []byte) (shapeSplit, bool) {
	_, _, first := shapeBounds(src)
	if first < 0 {
		return shapeSplit{}, false
	}
	head := bytes.TrimRight(src[:first], " \t\r\n")
	pkg := bytes.Index(head, []byte("package "))
	if pkg < 0 {
		return shapeSplit{}, false
	}
	return shapeSplit{goStart: pkg, goEnd: len(head), markup: first}, true
}

// headerFold is the fold of a shape's Go header: from the package clause to
// its last line of Go before the markup.
func headerFold(src []byte) (uint32, uint32, bool) {
	sp, ok := splitShape(src)
	if !ok {
		return 0, 0, false
	}
	start, end := positionOf(src, sp.goStart).Line, positionOf(src, sp.goEnd).Line
	return start, end, end > start
}

// withHeaderFold adds the header's fold to a shape's folding ranges.
func withHeaderFold(vf *vfile, folds []any) []any {
	if !isShape(vf) {
		return folds
	}
	start, end, ok := headerFold(vf.text())
	if !ok {
		return folds
	}
	for _, f := range folds {
		if m, _ := f.(map[string]any); m != nil && m["startLine"] == start {
			m["endLine"] = max(m["endLine"].(uint32), end)
			return folds
		}
	}
	return append([]any{map[string]any{"startLine": start, "endLine": end}}, folds...)
}
