package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"

	"github.com/vuka-lang/vuka/transpile"
)

// Go in a .vuka.html: holes, attribute values and statement lines. While a
// line of it is typed it is rarely Go yet ({item.La</a>, if item.), and a
// template that doesn't parse compiles blank, losing completion where it is
// wanted most. So the language server compiles the buffer with the Go at the
// cursor (where the editor's last change ended) completed: its brackets and
// hole closed, an operand put where one is missing, a statement given an
// empty body. The syntax check still reads the buffer as it is.

// tmplStmtLine is a template line of Go: a statement, or its else.
var tmplStmtLine = regexp.MustCompile(`^[ \t]*(if\s|for\s|switch\b|case\s|default\s*:|}\s*else\b|[\w, ]+:=)`)

// tmplGo is the Go around an offset of a template file.
type tmplGo struct {
	start int    // where the Go starts: after a hole's {, or the statement's first byte
	stmt  bool   // a statement line, else a hole or an attribute's value
	attr  bool   // an attribute's value
	open  []byte // the brackets open at the offset, outermost first; a hole's own { first
}

// templateGoAt is the Go of a template file around off, if off is in a hole,
// an attribute's value or a statement line of the markup (not the package
// clause, an import, a header, a comment, a script or a stylesheet).
func templateGoAt(src []byte, off int) (tmplGo, bool) {
	if off < 0 || off > len(src) || inTemplateAsset(src, off) {
		return tmplGo{}, false
	}
	ls := bytes.LastIndexByte(src[:off], '\n') + 1
	line := src[ls:off]
	trimmed := bytes.TrimLeft(line, " \t")
	for _, p := range []string{"func", "import", "package", "//"} {
		if bytes.HasPrefix(line, []byte(p)) || p == "//" && bytes.HasPrefix(trimmed, []byte(p)) {
			return tmplGo{}, false
		}
	}
	if tmplStmtLine.Match(line) {
		start := off - len(trimmed)
		if src[start] == '}' {
			start++ // the else's
		}
		open, ok := goBrackets(src[start:off])
		return tmplGo{start: start, stmt: true, open: open}, ok
	}
	for i := 0; i < len(line); i++ {
		if line[i] != '{' {
			continue
		}
		if i+1 < len(line) && line[i+1] == '{' {
			i++ // a literal brace
			continue
		}
		if end := holeEndBytes(line[i+1:]); end >= 0 {
			i += 1 + end // a hole closed before off
			continue
		}
		open, ok := goBrackets(line[i+1:])
		return tmplGo{start: ls + i + 1, attr: i > 0 && line[i-1] == '=', open: append([]byte{'{'}, open...)}, ok
	}
	return tmplGo{}, false
}

// goBrackets are the brackets left open at the end of the Go text s,
// outermost first; false inside a string, or where a bracket closes one it
// doesn't match.
func goBrackets(s []byte) ([]byte, bool) {
	var open []byte
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\'', '`':
			if i = closeQuote(s, i); i >= len(s) {
				return nil, false
			}
		case '(', '[', '{':
			open = append(open, c)
		case ')', ']', '}':
			if len(open) == 0 || open[len(open)-1] != opener(c) {
				return nil, false
			}
			open = open[:len(open)-1]
		}
	}
	return open, true
}

// closeQuote is the index of the quote closing the one at s[i], or len(s).
func closeQuote(s []byte, i int) int {
	c := s[i]
	for i++; i < len(s) && s[i] != c; i++ {
		if s[i] == '\\' && c != '`' {
			i++
		}
	}
	return min(i, len(s))
}

// holeEndBytes is the index of the } closing a hole whose Go starts s, or -1.
func holeEndBytes(s []byte) int {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\'', '`':
			i = closeQuote(s, i)
		case '{':
			depth++
		case '}':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

func opener(c byte) byte {
	switch c {
	case ')':
		return '('
	case ']':
		return '['
	}
	return '{'
}

func closer(c byte) byte {
	switch c {
	case '(':
		return ')'
	case '[':
		return ']'
	}
	return '}'
}

// inTemplateAsset reports whether off is inside a <script> or <style>
// element of a template file.
func inTemplateAsset(src []byte, off int) bool {
	before := src[:off]
	for _, kind := range []string{"script", "style"} {
		o := bytes.LastIndex(before, []byte("<"+kind))
		if o >= 0 && o+len(kind)+1 < len(src) && !isIdentByte(src[o+len(kind)+1]) && bytes.LastIndex(before, []byte("</"+kind+">")) < o {
			return true
		}
	}
	return false
}

// needsOperand matches Go that ends where an operand must follow.
var needsOperand = regexp.MustCompile(`(?:^|[^\w.])(?:if|range|case|return|switch)\s*$|(?::=|[-+*/%&|^<>=!]|&&|\|\||<-)\s*$`)

// templateRepairs are the insertions that make the Go at the cursor of a
// template file compile mid-edit; nil when the cursor is not in Go.
func templateRepairs(src []byte, cursor int) []insert {
	g, ok := templateGoAt(src, cursor)
	if !ok {
		return nil
	}
	ls := bytes.LastIndexByte(src[:cursor], '\n') + 1
	lineEnd := len(src)
	if i := bytes.IndexByte(src[cursor:], '\n'); i >= 0 {
		lineEnd = cursor + i
	}
	for lineEnd > cursor && strings.IndexByte(" \t\r", src[lineEnd-1]) >= 0 {
		lineEnd--
	}
	rest := src[cursor:lineEnd]
	if g.stmt && bytes.HasSuffix(rest, []byte("{")) {
		rest = rest[:len(rest)-1] // the statement's body
	}
	before := bytes.TrimRight(src[g.start:cursor], " \t")
	after := bytes.TrimLeft(rest, " \t")
	var at strings.Builder // what goes at the cursor
	operandEnds := len(after) == 0 || strings.IndexByte(")]},:;{<", after[0]) >= 0
	switch {
	case operandEnds && len(before) == 0 && !g.stmt, // an empty hole
		operandEnds && len(before) > 0 && needsOperand.Match(before),
		len(before) > 0 && before[len(before)-1] == '.' && len(src[g.start:cursor]) == len(before) &&
			(len(after) == 0 || !isIdentByte(after[0])):
		at.WriteString(placeholder)
	}
	skipped, still, stop := missingClosers(rest, g.open, !g.stmt, g.attr)
	at.Write(skipped)
	var out []insert
	if at.Len() > 0 {
		out = append(out, insert{cursor, at.String()})
	}
	if len(still) > 0 {
		out = append(out, insert{cursor + stop, string(still)})
	}
	if g.stmt {
		line := strings.TrimSpace(string(src[ls:lineEnd]))
		kw := strings.TrimLeft(line, "} \t")
		switch {
		case strings.HasPrefix(kw, "case") || strings.HasPrefix(kw, "default"):
			if !strings.HasSuffix(line, ":") {
				out = append(out, insert{lineEnd, ":"})
			}
		case strings.HasPrefix(kw, "if") || strings.HasPrefix(kw, "for") || strings.HasPrefix(kw, "switch") || strings.HasPrefix(kw, "else"):
			if !strings.HasSuffix(line, "{") {
				out = append(out, insert{lineEnd, " {}"})
			}
		}
	}
	return out
}

// missingClosers reads the rest of the line after the cursor, open the
// brackets open there: skipped are the closers of those it closes an outer
// one past (they go at the cursor), still those of the ones it leaves open
// (they go at stop, where a hole's Go ends: a tag, its tag's end for an
// attribute's value, or the line's end). Both innermost first.
func missingClosers(rest, open []byte, hole, attr bool) (skipped, still []byte, stop int) {
	stack := append([]byte(nil), open...)
	depth := 0
	stop = len(rest)
	for i := 0; i < len(rest) && len(stack) > 0; i++ {
		switch c := rest[i]; c {
		case '"', '\'', '`':
			i = closeQuote(rest, i)
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
				continue
			}
			k := len(stack) - 1
			for k >= 0 && stack[k] != opener(c) {
				k--
			}
			if k < 0 {
				continue
			}
			for j := len(stack) - 1; j > k; j-- {
				skipped = append(skipped, closer(stack[j]))
			}
			stack = stack[:k]
		case '<':
			if hole && depth == 0 && i+1 < len(rest) && (rest[i+1] == '/' || isLetter(rest[i+1])) {
				stop = i
				i = len(rest)
			}
		case '>', '/':
			// An attribute's value ends with its tag.
			if attr && depth == 0 && (c == '>' || i+1 < len(rest) && rest[i+1] == '>') {
				stop = i
				i = len(rest)
			}
		}
	}
	for stop > 0 && (rest[stop-1] == ' ' || rest[stop-1] == '\t') {
		stop--
	}
	return skipped, reverseClosers(stack), stop
}

// reverseClosers are the closers of open, innermost first.
func reverseClosers(open []byte) []byte {
	out := make([]byte, 0, len(open))
	for i := len(open) - 1; i >= 0; i-- {
		out = append(out, closer(open[i]))
	}
	return out
}

// mainTemplate reports whether off in a template file is in its main
// template, the markup before the first func line: where bare names are the
// component's fields and methods.
func mainTemplate(src []byte, off int) bool {
	start := 0
	for _, l := range strings.SplitAfter(string(src), "\n") {
		if start > off {
			return true
		}
		if strings.HasPrefix(l, "func") && (len(l) == 4 || !isIdentByte(l[4])) {
			return false
		}
		start += len(l)
	}
	return true
}

// receiverAt is the receiver of the method of the generated Go at off: its
// name and its type as Go writes it.
func receiverAt(gen []byte, off int) (name, typ string, ok bool) {
	fset := token.NewFileSet()
	f, _ := parser.ParseFile(fset, "", gen, parser.SkipObjectResolution)
	if f == nil {
		return "", "", false
	}
	tf := fset.File(f.Pos())
	if tf == nil || off > tf.Size() {
		return "", "", false
	}
	pos := tf.Pos(off)
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || len(fn.Recv.List[0].Names) != 1 || pos < fn.Pos() || pos > fn.End() {
			continue
		}
		r := fn.Recv.List[0]
		return r.Names[0].Name, string(gen[tf.Offset(r.Type.Pos()):tf.Offset(r.Type.End())]), true
	}
	return "", "", false
}

// tmplSpot is a completion asked in the Go of a template file.
type tmplSpot struct {
	genPath    string
	main       bool     // in the main template, where bare names are the receiver's
	recv, typ  string   // the main template's receiver in the generated Go: s, Sales
	members    bool     // a bare name is typed: the receiver's fields and methods complete it
	typed      lspRange // the name typed so far
	importLine bool     // in the imports, where packages are paths to complete
}

// templateSpot is the completion at off of the template file vf as Go, or
// nil when off is in markup.
func templateSpot(vf *vfile, genPath string, off int) *tmplSpot {
	src := vf.text()
	if !strings.HasSuffix(vf.source, ".vuka.html") || inTemplateAsset(src, off) {
		return nil
	}
	line := bytes.Count(src[:off], []byte("\n"))
	if line < transpile.ParseTemplateHead(src).Top {
		return &tmplSpot{genPath: genPath, importLine: true}
	}
	s := &tmplSpot{genPath: genPath}
	start := off
	for start > 0 && isIdentByte(src[start-1]) {
		start--
	}
	s.typed = lspRange{positionOf(src, start), positionOf(src, off)}
	if _, ok := templateGoAt(src, off); ok && mainTemplate(src, off) {
		gen := offsetOf(vf.gen, vf.toGen(positionOf(src, off)))
		if recv, typ, ok := receiverAt(vf.gen, gen); ok {
			s.main, s.recv, s.typ = true, recv, typ
			s.members = start == 0 || src[start-1] != '.'
		}
	}
	return s
}

// templateCompletion is gopls's completion v in a template file as a Go
// file of its package would have it: the generated file's own names (its
// receiver where the template names none, imports the template doesn't
// make) left out, and at a bare name the receiver's fields and methods first.
func (p *proxy) templateCompletion(vf *vfile, s *tmplSpot, v any) any {
	if s.importLine {
		return v
	}
	visible := map[string]bool{}
	for _, imp := range transpile.ParseTemplateHead(vf.text()).Imports {
		visible[specName(importSpec{imp.Name, imp.Path})] = true
	}
	for _, f := range p.packageSources(vf.source) {
		for _, imp := range importList(f.Src) {
			visible[specName(imp)] = true
		}
	}
	hidden := map[string]bool{}
	for _, imp := range importList(vf.gen) {
		if n := specName(imp); !visible[n] && n != "ui" {
			hidden[n] = true
		}
	}
	list, _ := v.(map[string]any)
	var arr []any
	if list != nil {
		arr, _ = list["items"].([]any)
	} else {
		arr, _ = v.([]any)
	}
	kept := make([]any, 0, len(arr))
	for _, it := range arr {
		item, ok := it.(map[string]any)
		if !ok {
			continue
		}
		label, _ := item["label"].(string)
		head, _, qualified := strings.Cut(label, ".")
		if hidden[head] && item["additionalTextEdits"] == nil {
			continue
		}
		if s.main && s.recv != "" {
			if label == s.recv && !qualified {
				continue
			}
			if head == s.recv && qualified {
				unqualify(item, s.recv+".")
			}
		}
		kept = append(kept, item)
	}
	if s.members {
		members := p.memberItems(s)
		if len(members) > 0 {
			kept = mergeMembers(members, kept)
		}
	}
	if list == nil {
		list = map[string]any{"isIncomplete": true}
	}
	list["items"] = kept
	return list
}

// specName is the name an import declares.
func specName(s importSpec) string {
	if s.name != "" {
		return s.name
	}
	return s.path[strings.LastIndexByte(s.path, '/')+1:]
}

// unqualify writes a completion of recv.X as the template's bare X.
func unqualify(item map[string]any, q string) {
	for _, k := range []string{"label", "filterText", "insertText"} {
		if t, ok := item[k].(string); ok {
			item[k] = strings.TrimPrefix(t, q)
		}
	}
	if te, ok := item["textEdit"].(map[string]any); ok {
		if t, ok := te["newText"].(string); ok {
			te["newText"] = strings.TrimPrefix(t, q)
		}
	}
}

// memberItems are the fields and methods of the main template's receiver
// completing the name typed at s, as items replacing it.
func (p *proxy) memberItems(s *tmplSpot) []map[string]any {
	expr := "(*new(" + s.typ + "))."
	if t, ok := strings.CutPrefix(s.typ, "*"); ok {
		expr = "new(" + t + ")."
	}
	p.mu.Lock()
	vf := p.vfileOf(s.genPath)
	var typed string
	if vf != nil {
		src := vf.text()
		typed = string(src[offsetOf(src, s.typed.Start):offsetOf(src, s.typed.End)])
	}
	p.mu.Unlock()
	if vf == nil {
		return nil
	}
	var out []map[string]any
	for _, it := range p.probe(s.genPath, expr+typed, "") {
		label, _ := it["label"].(string)
		if strings.Contains(label, ".") || strings.HasPrefix(label, "__") {
			continue // a deep completion, or Vuka's
		}
		text := label
		if te, ok := it["textEdit"].(map[string]any); ok {
			if t, ok := te["newText"].(string); ok {
				text = t
			}
		} else if t, ok := it["insertText"].(string); ok {
			text = t
		}
		item := map[string]any{"label": label, "kind": it["kind"], "filterText": label,
			"textEdit": map[string]any{"range": s.typed, "newText": text}}
		for _, k := range []string{"detail", "documentation", "insertTextFormat", "tags", "command"} {
			if x, ok := it[k]; ok {
				item[k] = x
			}
		}
		sort, _ := it["sortText"].(string)
		item["sortText"] = "0" + sort
		out = append(out, item)
	}
	return out
}

// mergeMembers puts the receiver's members before gopls's other items: a
// member wins over a package-level name of its spelling, a local (a
// variable) over the member.
func mergeMembers(members []map[string]any, items []any) []any {
	local := map[string]bool{}
	for _, it := range items {
		item := it.(map[string]any)
		if k, _ := item["kind"].(float64); k == 6 {
			label, _ := item["label"].(string)
			local[label] = true
		}
	}
	byLabel := map[string]bool{}
	out := make([]any, 0, len(members)+len(items))
	for _, m := range members {
		label := m["label"].(string)
		if local[label] {
			continue
		}
		byLabel[label] = true
		out = append(out, m)
	}
	for _, it := range items {
		item := it.(map[string]any)
		label, _ := item["label"].(string)
		if byLabel[label] {
			continue
		}
		sort, _ := item["sortText"].(string)
		item["sortText"] = "1" + sort
		item["preselect"] = nil
		delete(item, "preselect")
		out = append(out, item)
	}
	return out
}
