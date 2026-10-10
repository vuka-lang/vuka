package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"strings"
)

// The editor's view of JSX: the markup is read here, from the .vuka text, for
// what gopls can't see in the generated Go — closing tags and attribute names
// are written out of it, and a tag being typed isn't Go at all.

// jsxTag is a tag in a .vuka file's markup, read as far as it goes.
type jsxTag struct {
	start, end         int // the <, and just after the > or the last thing an unfinished tag holds
	stop               int // where an unfinished tag stops: the next tag, an unbalanced brace, the end
	name               string
	nameStart, nameEnd int
	close, self, done  bool
	attrs              []jsxAttrAt
}

type jsxAttrAt struct {
	name       string
	start, end int // the name
	valStart   int // the value, quotes or braces included; 0 when none
	valEnd     int
	expr       bool // the value is {…}
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func isLetter(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

func isNameByte(c byte) bool { return isIdentByte(c) || c == '-' || c == ':' || c == '.' }

// isComponentTag reports whether a tag names a Go function: Capitalized, or pkg.Name.
func isComponentTag(tag string) bool {
	return tag != "" && ('A' <= tag[0] && tag[0] <= 'Z' || strings.Contains(tag, "."))
}

// readTag reads the tag whose < is at i; false when there is none there.
func readTag(src []byte, i int) (jsxTag, bool) {
	t := jsxTag{start: i}
	j := i + 1
	if j < len(src) && src[j] == '/' {
		t.close = true
		j++
	}
	t.nameStart = j
	for j < len(src) && isNameByte(src[j]) {
		j++
	}
	t.nameEnd, t.name = j, string(src[t.nameStart:j])
	if j < len(src) && src[j] == '[' && t.name != "" && !t.close { // a generic component: <List[User]>
		if e := strings.IndexAny(string(src[j:]), "]\n<>"); e > 0 && src[j+e] == ']' {
			j += e + 1
		}
	}
	if t.name != "" && !isLetter(t.name[0]) || t.name == "" && j < len(src) && !isSpace(src[j]) && src[j] != '>' {
		return t, false
	}
	stop := func(at int) (jsxTag, bool) {
		t.stop, t.end = at, at
		for t.end > t.nameEnd && isSpace(src[t.end-1]) {
			t.end--
		}
		return t, true
	}
	for {
		for j < len(src) && isSpace(src[j]) {
			j++
		}
		if j >= len(src) {
			return stop(j)
		}
		switch c := src[j]; {
		case c == '>':
			t.end, t.stop, t.done = j+1, j+1, true
			return t, true
		case c == '/' && j+1 < len(src) && src[j+1] == '>' && !t.close:
			t.end, t.stop, t.done, t.self = j+2, j+2, true, true
			return t, true
		case t.close:
			return stop(j)
		case c == '{':
			e := skipGo(src, j)
			if e < 0 {
				return stop(j)
			}
			j = e
		case isLetter(c) || c == '_':
			a := jsxAttrAt{start: j}
			for j < len(src) && isNameByte(src[j]) {
				j++
			}
			a.end, a.name = j, string(src[a.start:j])
			k := j
			for k < len(src) && isSpace(src[k]) {
				k++
			}
			if k < len(src) && src[k] == '=' {
				k++
				for k < len(src) && isSpace(src[k]) {
					k++
				}
				switch {
				case k < len(src) && (src[k] == '"' || src[k] == '\''):
					e := strings.IndexByte(string(src[k+1:]), src[k])
					if e < 0 {
						t.attrs = append(t.attrs, a)
						return stop(k)
					}
					a.valStart, a.valEnd, j = k, k+e+2, k+e+2
				case k < len(src) && src[k] == '{':
					e := skipGo(src, k)
					if e < 0 {
						t.attrs = append(t.attrs, a)
						return stop(k)
					}
					a.valStart, a.valEnd, a.expr, j = k, e, true, e
				default:
					j = k
				}
			}
			t.attrs = append(t.attrs, a)
		default:
			return stop(j)
		}
	}
}

// skipGo returns the offset just after the } closing the { at i, skipping Go
// strings, runes and comments; -1 when it isn't closed.
func skipGo(src []byte, i int) int {
	depth := 0
	for ; i < len(src); i++ {
		switch c := src[i]; c {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return i + 1
			}
		case '"', '\'':
			for i++; i < len(src) && src[i] != c && src[i] != '\n'; i++ {
				if src[i] == '\\' {
					i++
				}
			}
		case '`':
			for i++; i < len(src) && src[i] != '`'; i++ {
			}
		case '/':
			if i+1 < len(src) && src[i+1] == '/' {
				for i < len(src) && src[i] != '\n' {
					i++
				}
			} else if i+1 < len(src) && src[i+1] == '*' {
				e := strings.Index(string(src[i+2:]), "*/")
				if e < 0 {
					return -1
				}
				i += e + 3
			}
		}
	}
	return -1
}

// markupStarts reports whether the < at i can only open markup: it follows
// what Go allows an operand after (return ( , = { : [ ;), the end of a tag or a
// {…} child, or starts its line.
func markupStarts(src []byte, i int) bool {
	k := i - 1
	for k >= 0 && (src[k] == ' ' || src[k] == '\t') {
		k--
	}
	if k < 0 || src[k] == '\n' {
		return true
	}
	if strings.IndexByte("(,={:[;>}", src[k]) >= 0 {
		return !(src[k] == '=' && k > 0 && strings.IndexByte("=!<>", src[k-1]) >= 0)
	}
	return k >= 5 && string(src[k-5:k+1]) == "return" && (k == 5 || !isIdentByte(src[k-6]))
}

// tagAt reports whether the < at i looks like a tag's: a name or / right
// after it, and no operand right before it.
func tagAt(src []byte, i int) bool {
	if i+1 >= len(src) || src[i] != '<' {
		return false
	}
	c := src[i+1]
	if c == '/' && i+2 < len(src) {
		c = src[i+2]
	}
	return isLetter(c) && (i == 0 || !isIdentByte(src[i-1]) && src[i-1] != ')' && src[i-1] != ']')
}

// blankUnfinishedTags blanks out each tag still being typed (no > yet),
// keeping the {…} values of its attributes, so the rest of the file transpiles
// mid-edit and the values keep their completion. Lengths and lines stay.
func blankUnfinishedTags(src []byte) []byte {
	out := src
	for i := 0; i < len(src); i++ {
		if src[i] != '<' || i+1 < len(src) && src[i+1] == '/' || !markupStarts(src, i) {
			continue
		}
		t, ok := readTag(src, i)
		if !ok {
			continue
		}
		if t.done {
			i = t.nameEnd - 1
			continue
		}
		if &out[0] == &src[0] {
			out = append([]byte(nil), src...)
		}
		keep := func(at int) bool {
			for _, a := range t.attrs {
				if a.expr && at >= a.valStart && at < a.valEnd {
					return true
				}
			}
			return false
		}
		for j := t.start; j < t.end; j++ {
			if !isSpace(src[j]) && !keep(j) {
				out[j] = ' '
			}
		}
		i = t.end - 1
	}
	return out
}

// danglingDots are the offsets just after a selector's dot with nothing after
// it yet (u. before a } ) ] , or the line's end), where a placeholder makes the
// expression Go again.
func danglingDots(src []byte) []int {
	var out []int
	for i := 1; i < len(src); i++ {
		if src[i] != '.' || src[i-1] == '.' || i+1 < len(src) && src[i+1] == '.' {
			continue
		}
		j := i + 1
		for j < len(src) && (src[j] == ' ' || src[j] == '\t') {
			j++
		}
		if j < len(src) && strings.IndexByte("})],\r\n", src[j]) < 0 {
			continue
		}
		k := i
		for k > 0 && isIdentByte(src[k-1]) {
			k--
		}
		if k == i && src[i-1] != ')' && src[i-1] != ']' || k < i && !isLetter(src[k]) && src[k] != '_' {
			continue
		}
		line := src[strings.LastIndexByte(string(src[:i]), '\n')+1 : i]
		if strings.Contains(string(line), "//") || strings.Count(string(line), `"`)%2 == 1 || attrPrefix.Match(line) {
			continue
		}
		out = append(out, i+1)
	}
	return out
}

// tagAround is the innermost tag whose attributes hold off.
func tagAround(src []byte, off int) (jsxTag, bool) {
	for i := min(off, len(src)) - 1; i >= 0; i-- {
		if src[i] != '<' || !tagAt(src, i) || src[i+1] == '/' {
			continue
		}
		t, ok := readTag(src, i)
		if ok && off > t.nameEnd && (t.done && off < t.end || !t.done && (off <= t.stop || openQuote(src, t.stop, off))) {
			return t, true
		}
	}
	return jsxTag{}, false
}

// openQuote reports whether a quote at q opens a value still being typed at off.
func openQuote(src []byte, q, off int) bool {
	return q < len(src) && (src[q] == '"' || src[q] == '\'') && off <= len(src) && !strings.ContainsAny(string(src[q+1:off]), "\n\"'")
}

// closeTagAt is the closing tag whose name holds off.
func closeTagAt(src []byte, off int) (jsxTag, bool) {
	s := min(off, len(src))
	for s > 0 && isNameByte(src[s-1]) {
		s--
	}
	if s < 2 || src[s-1] != '/' || src[s-2] != '<' {
		return jsxTag{}, false
	}
	t, ok := readTag(src, s-2)
	return t, ok && t.name != "" && off <= t.nameEnd
}

// matchOpen is the opening tag a closing tag closes.
func matchOpen(src []byte, c jsxTag) (jsxTag, bool) {
	depth := 0
	for i := c.start - 1; i >= 0; i-- {
		if src[i] != '<' || !tagAt(src, i) {
			continue
		}
		t, ok := readTag(src, i)
		switch {
		case !ok || t.name != c.name || !t.done:
		case t.close:
			depth++
		case t.self:
		case depth == 0:
			return t, true
		default:
			depth--
		}
	}
	return jsxTag{}, false
}

// matchClose is the closing tag of an opening tag.
func matchClose(src []byte, o jsxTag) (jsxTag, bool) {
	depth := 0
	for i := o.end; i < len(src); i++ {
		if src[i] != '<' || !tagAt(src, i) {
			continue
		}
		t, ok := readTag(src, i)
		switch {
		case !ok || t.name != o.name || !t.done || t.self:
		case !t.close:
			depth++
		case depth == 0:
			return t, true
		default:
			depth--
		}
	}
	return jsxTag{}, false
}

// closeTwin is, for a range on an opening tag's name, the same range on its
// closing tag: what a rename or a reference of the component also covers.
func closeTwin(src []byte, r lspRange) (lspRange, bool) {
	s, e := offsetOf(src, r.Start), offsetOf(src, r.End)
	i := s
	for i > 0 && isNameByte(src[i-1]) {
		i--
	}
	if i == 0 || src[i-1] != '<' {
		return r, false
	}
	o, ok := readTag(src, i-1)
	if !ok || !o.done || o.self || o.close || e > o.nameEnd {
		return r, false
	}
	c, ok := matchClose(src, o)
	if !ok {
		return r, false
	}
	return lspRange{positionOf(src, c.nameStart+s-o.nameStart), positionOf(src, c.nameStart+e-o.nameStart)}, true
}

// bindName finds an attribute's parameter or field as the transpiler does: the
// same name, or one differing only in its first letter's case.
func bindName(names []string, attr string) int {
	for i, n := range names {
		if n == attr {
			return i
		}
	}
	for i, n := range names {
		if n != "" && n != "_" && attr != "" && strings.EqualFold(n[:1], attr[:1]) && n[1:] == attr[1:] {
			return i
		}
	}
	return -1
}

// jsxSpot is where in markup a completion was asked.
type jsxSpot struct {
	kind string // "tag", "close", "attr" or "value"
	tag  jsxTag // for attr and value: the tag
	attr string // for value: the attribute
	lt   int    // for tag and close: the <
	from int    // the start of what's typed
	qual string // for tag: a package typed before the name (pkg.)
}

func completionSpot(src []byte, off int) (jsxSpot, bool) {
	s := off
	for s > 0 && isNameByte(src[s-1]) {
		s--
	}
	typed := string(src[s:off])
	switch {
	case s > 1 && src[s-1] == '/' && src[s-2] == '<':
		return jsxSpot{kind: "close", lt: s - 2, from: s}, true
	case s > 0 && src[s-1] == '<' && (markupStarts(src, s-1) || typed != "" && tagAt(src, s-1)):
		if typed != "" && !isLetter(typed[0]) {
			return jsxSpot{}, false
		}
		dot := strings.LastIndexByte(typed, '.')
		return jsxSpot{kind: "tag", lt: s - 1, from: s + dot + 1, qual: typed[:dot+1]}, true
	}
	t, ok := tagAround(src, off)
	if !ok || t.close {
		return jsxSpot{}, false
	}
	valueSpot := func(attr string) (jsxSpot, bool) {
		if isComponentTag(t.name) {
			return jsxSpot{}, false
		}
		w := off
		for w > 0 && !isSpace(src[w-1]) && src[w-1] != '"' && src[w-1] != '\'' {
			w--
		}
		return jsxSpot{kind: "value", tag: t, attr: attr, from: w}, true
	}
	for _, a := range t.attrs {
		if a.valStart > 0 && off > a.valStart && off < a.valEnd {
			if a.expr {
				return jsxSpot{}, false // in a value: Go
			}
			return valueSpot(a.name)
		}
	}
	if n := len(t.attrs); !t.done && n > 0 && t.attrs[n-1].valStart == 0 && t.stop < len(src) && (src[t.stop] == '"' || src[t.stop] == '\'') && off > t.stop {
		return valueSpot(t.attrs[n-1].name) // in a value not closed yet
	}
	w := off
	for w > 0 && isNameByte(src[w-1]) {
		w--
	}
	if w > 0 && !isSpace(src[w-1]) {
		return jsxSpot{}, false
	}
	return jsxSpot{kind: "attr", tag: t, from: w}, true
}

// completeJSX answers a completion in markup.
func (p *proxy) completeJSX(vf *vfile, genPath string, spot jsxSpot, off int) any {
	src := vf.text()
	typed := lspRange{positionOf(src, spot.from), positionOf(src, off)}
	var items []any
	var last map[string]any
	item := func(label string, kind int, detail, text string, format int, sort string) {
		last = map[string]any{"label": label, "kind": kind, "sortText": sort, "filterText": label,
			"textEdit": map[string]any{"range": typed, "newText": text}, "insertTextFormat": format}
		if detail != "" {
			last["detail"] = detail
		}
		items = append(items, last)
	}
	doc := func(md string) {
		if md != "" {
			last["documentation"] = map[string]any{"kind": "markdown", "value": md}
		}
	}
	switch spot.kind {
	case "close":
		if e, ok := scanMarkup(src).unclosedAt(spot.lt); ok {
			name := e.open.name
			text := name
			if off >= len(src) || src[off] != '>' {
				text += ">"
			}
			label := name
			if name == "" {
				label = "</>"
			}
			item(label, 14, "", text, 1, "0")
			if h := htmlElements[name]; h != nil {
				doc(elementDoc(h))
			}
		}
	case "tag":
		structs := 0
		for _, it := range p.probe(genPath, spot.qual+string(src[spot.from:off]), "") {
			label, _ := it["label"].(string)
			detail, _ := it["detail"].(string)
			kind, _ := it["kind"].(float64)
			if _, extra := it["additionalTextEdits"]; extra {
				continue
			}
			switch {
			case (kind == 2 || kind == 3) && isExported(label) && returnsNode(detail):
				item(label, 3, detail, label, 1, "0"+label)
			case kind == 22 && isExported(label) && structs < 20:
				structs++
				if p.statefulAttrs(genPath, spot.qual+label) != nil {
					item(label, 7, "stateful component", label, 1, "0"+label)
				}
			case kind == 9 && spot.qual == "":
				item(label, 9, detail, label, 1, "2"+label)
			}
		}
		if spot.qual == "" {
			for i, t := range htmlElemNames {
				e := htmlElements[t]
				detail := "HTML element"
				if e.svg {
					detail = "SVG element"
				}
				item(t, 14, detail, t, 1, fmt.Sprintf("1%03d", i))
				doc(elementDoc(e))
			}
		}
	case "attr":
		used := map[string]bool{}
		for _, a := range spot.tag.attrs {
			if off < a.start || off > a.end {
				used[strings.ToLower(a.name[:1])+a.name[1:]] = true
			}
		}
		if !isComponentTag(spot.tag.name) {
			tag := spot.tag.name
			own, global := elementAttrs(tag)
			attr := func(group string, i int, a string) {
				if used[a] {
					return
				}
				text, format := a+`="$1"`, 2
				switch {
				case htmlBooleans[a]:
					text, format = a, 1
				case a == "style" || a == "key":
					text = a + "={$1}"
				}
				item(a, 5, "", text, format, fmt.Sprintf("%s%03d", group, i))
				doc(attrDoc(tag, a))
				if len(attrValues(tag, a)) > 0 {
					last["command"] = map[string]any{"title": "values", "command": "editor.action.triggerSuggest"}
				}
			}
			for i, a := range own {
				attr("0", i, a)
			}
			for i, a := range global {
				attr("1", i, a)
			}
			if e := htmlElements[tag]; e == nil || !e.svg {
				for i, a := range htmlEventNames {
					if !used[a] {
						item(a, 23, "event handler", a+"={$1}", 2, fmt.Sprintf("2%03d", i))
						doc(attrDoc(tag, a))
					}
				}
				for i, a := range htmlAriaNames {
					attr("3", i, a)
				}
				item("data-", 5, "custom data attribute", `data-${1:name}="$2"`, 2, "4")
				doc(attrDoc(tag, "data-x"))
			}
			break
		}
		for i, a := range p.componentAttrs(genPath, spot.tag.name) {
			if used[strings.ToLower(a.name[:1])+a.name[1:]] {
				continue
			}
			text := a.name + "={$1}"
			switch a.typ {
			case "bool":
				text = a.name
			case "string":
				text = a.name + `="$1"`
			}
			item(a.name, 5, a.typ, text, 2, fmt.Sprintf("%02d", i))
		}
	case "value":
		for i, v := range attrValues(spot.tag.name, spot.attr) {
			item(v, 12, spot.attr, v, 1, fmt.Sprintf("%03d", i))
		}
	}
	return map[string]any{"isIncomplete": spot.kind == "tag", "items": items}
}

// returnsNode reports whether a function's detail says it returns a component:
// a vuka.Node or templ.Component, or one and an error.
func returnsNode(detail string) bool {
	ft := funcDetail(detail)
	if ft == nil || ft.Results == nil {
		return false
	}
	var res []string
	for _, f := range ft.Results.List {
		for range max(1, len(f.Names)) {
			res = append(res, types.ExprString(f.Type))
		}
	}
	if len(res) == 2 && res[1] == "error" {
		res = res[:1]
	}
	if len(res) != 1 {
		return false
	}
	r := res[0][strings.LastIndexByte(res[0], '.')+1:]
	return r == "Node" || r == "Component"
}

func funcDetail(detail string) *ast.FuncType {
	e, err := parser.ParseExpr(detail)
	if err != nil {
		return nil
	}
	ft, _ := e.(*ast.FuncType)
	return ft
}

type compAttr struct{ name, typ string }

// componentAttrs are the attributes a component takes, in declaration order:
// its parameters, or the fields of the struct it takes (and the parameter
// itself, which passes the struct whole).
func (p *proxy) componentAttrs(genPath, tag string) []compAttr {
	last := tag[strings.LastIndexByte(tag, '.')+1:]
	var ft *ast.FuncType
	for _, it := range p.probe(genPath, tag, "") {
		if it["label"] == last {
			if kind, _ := it["kind"].(float64); kind == 22 {
				return p.statefulAttrs(genPath, tag)
			}
			detail, _ := it["detail"].(string)
			ft = funcDetail(detail)
			break
		}
	}
	if ft == nil {
		return nil
	}
	var params []compAttr
	var only ast.Expr
	for _, f := range ft.Params.List {
		for _, n := range f.Names {
			params = append(params, compAttr{n.Name, types.ExprString(f.Type)})
		}
		only = f.Type
	}
	var out []compAttr
	if len(params) == 1 && params[0].name != "children" {
		variadic := false
		if el, ok := only.(*ast.Ellipsis); ok {
			only, variadic = el.Elt, true
		}
		for _, it := range p.probe(genPath, types.ExprString(only)+"{", "}") {
			label, _ := it["label"].(string)
			if kind, _ := it["kind"].(float64); kind == 5 && label != "Children" {
				detail, _ := it["detail"].(string)
				out = append(out, compAttr{strings.ToLower(label[:1]) + label[1:], detail})
			}
		}
		if len(out) > 0 {
			if !variadic {
				out = append(out, params[0])
			}
			return out
		}
	}
	for _, a := range params {
		if a.name != "children" && a.name != "_" {
			out = append(out, a)
		}
	}
	return out
}

// statefulAttrs are the props of a stateful component, its exported fields
// (promoted ones too) as declared; nil when the struct isn't one: its pointer
// has no Render method, or no Subscribe promoted from vuka.Live.
func (p *proxy) statefulAttrs(genPath, tag string) []compAttr {
	var out []compAttr
	render, live := false, false
	for _, it := range p.probe(genPath, "(&"+tag+"{}).", "") {
		label, _ := it["label"].(string)
		detail, _ := it["detail"].(string)
		switch kind, _ := it["kind"].(float64); {
		case kind == 2 && label == "Render":
			render = true
		case kind == 2 && label == "Subscribe":
			live = true
		case kind == 5 && isExported(label) && label != "Live" && label != "Children":
			out = append(out, compAttr{label, detail})
		}
	}
	if !render || !live {
		return nil
	}
	if out == nil {
		out = []compAttr{}
	}
	return out
}

// probe asks gopls to complete code typed into a function added to the end of
// a generated file, where it sees the file's imports and its package; the file
// is put back after.
func (p *proxy) probe(genPath, before, after string) []map[string]any {
	p.genMu.Lock()
	defer p.genMu.Unlock()
	p.mu.Lock()
	vf := p.vfileOf(genPath) // never an editor-owned buffer
	if vf == nil {
		p.mu.Unlock()
		return nil
	}
	text := string(vf.gen) + "\nfunc _() {\n\t_ = " + before
	pos := positionOf([]byte(text), len(text))
	text += after + "\n}\n"
	vf.version += 2
	vf.probed = vf.version - 1
	v, gen := vf.version, string(vf.gen)
	p.mu.Unlock()
	uri := pathToURI(genPath)
	change := func(version int, s string) {
		_ = p.gopls.send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didChange", "params": map[string]any{
			"textDocument": map[string]any{"uri": uri, "version": version}, "contentChanges": []any{map[string]any{"text": s}}}})
	}
	change(v-1, text)
	res, ok := p.ask("textDocument/completion", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos})
	change(v, gen)
	if !ok {
		return nil
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if json.Unmarshal(res, &list) != nil {
		_ = json.Unmarshal(res, &list.Items)
	}
	return list.Items
}

// location reads an LSP Location or LocationLink.
func location(v any) (string, lspRange, bool) {
	m, _ := v.(map[string]any)
	if uri, ok := m["targetUri"].(string); ok {
		r, ok := toRange(m["targetSelectionRange"])
		return uri, r, ok
	}
	uri, _ := m["uri"].(string)
	r, ok := toRange(m["range"])
	return uri, r, ok && uri != ""
}

// definitionAt asks gopls where the identifier at pos in a Go file is declared.
func (p *proxy) definitionAt(uri string, pos lspPosition) (string, lspRange, bool) {
	res, ok := p.ask("textDocument/definition", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos})
	if !ok {
		return "", lspRange{}, false
	}
	var v any
	_ = json.Unmarshal(res, &v)
	if list, ok := v.([]any); ok && len(list) > 0 {
		v = list[0]
	}
	return location(v)
}

// goFile is the Go gopls has for a file: a generated one's, or the file's own.
func (p *proxy) goFile(uri string) []byte {
	path := uriToPath(uri)
	p.mu.Lock()
	vf := p.virtual[path]
	p.mu.Unlock()
	if vf != nil {
		return vf.gen
	}
	b, _ := os.ReadFile(path)
	return b
}

// attrTarget is where the parameter or props field an attribute of a
// component's tag binds to is declared, in Go gopls knows.
func (p *proxy) attrTarget(vf *vfile, genPath string, tag jsxTag, attr string) (string, lspRange, bool) {
	at := tag.nameStart + strings.LastIndexByte(tag.name, '.') + 1
	uri, r, ok := p.definitionAt(pathToURI(genPath), vf.toGen(positionOf(vf.text(), at)))
	if !ok {
		return "", r, false
	}
	src := p.goFile(uri)
	fset := token.NewFileSet()
	file, _ := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if file == nil {
		return "", r, false
	}
	off := offsetOf(src, r.Start)
	var fn *ast.FuncDecl
	for _, d := range file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok && fset.Position(f.Name.Pos()).Offset == off {
			fn = f
		}
	}
	if fn == nil {
		if field := typeField(file, fset, off, attr); field != nil { // a stateful component's prop
			o := fset.Position(field.Pos()).Offset
			return uri, lspRange{positionOf(src, o), positionOf(src, o+len(field.Name))}, true
		}
		return "", r, false
	}
	var names []*ast.Ident
	var only ast.Expr
	for _, f := range fn.Type.Params.List {
		names = append(names, f.Names...)
		only = f.Type
	}
	identRange := func(src []byte, id *ast.Ident, fset *token.FileSet) lspRange {
		o := fset.Position(id.Pos()).Offset
		return lspRange{positionOf(src, o), positionOf(src, o+len(id.Name))}
	}
	var strs []string
	for _, n := range names {
		strs = append(strs, n.Name)
	}
	param := bindName(strs, attr)
	if len(names) == 1 && names[0].Name != "children" {
		if el, ok := only.(*ast.Ellipsis); ok {
			only = el.Elt
		}
		var id *ast.Ident
		switch t := only.(type) {
		case *ast.Ident:
			id = t
		case *ast.SelectorExpr:
			id = t.Sel
		}
		if id != nil {
			if tu, tr, ok := p.definitionAt(uri, identRange(src, id, fset).Start); ok {
				tsrc := p.goFile(tu)
				tset := token.NewFileSet()
				if tf, _ := parser.ParseFile(tset, "", tsrc, parser.SkipObjectResolution); tf != nil {
					if field := typeField(tf, tset, offsetOf(tsrc, tr.Start), attr); field != nil {
						return tu, identRange(tsrc, field, tset), true
					}
				}
			}
		}
	}
	if param < 0 {
		return "", r, false
	}
	return uri, identRange(src, names[param], fset), true
}

// typeField is the field attr names in the struct type declared at off.
func typeField(file *ast.File, fset *token.FileSet, off int, attr string) *ast.Ident {
	var field *ast.Ident
	ast.Inspect(file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || fset.Position(ts.Name.Pos()).Offset != off {
			return field == nil
		}
		if st, ok := ts.Type.(*ast.StructType); ok {
			var fields []*ast.Ident
			var fnames []string
			for _, f := range st.Fields.List {
				for _, n := range f.Names {
					fields = append(fields, n)
					fnames = append(fnames, n.Name)
				}
			}
			if i := bindName(fnames, attr); i >= 0 {
				field = fields[i]
			}
		}
		return false
	})
	return field
}

// attrAt is the component tag and attribute whose name holds off.
func attrAt(src []byte, off int) (jsxTag, jsxAttrAt, bool) {
	t, ok := tagAround(src, off)
	if !ok || t.close || !isComponentTag(t.name) {
		return t, jsxAttrAt{}, false
	}
	for _, a := range t.attrs {
		if off >= a.start && off <= a.end {
			return t, a, true
		}
	}
	return t, jsxAttrAt{}, false
}

// jsxRequest answers, itself, a request on markup gopls can't see: completion
// in a tag, and hover or definition on a component's attribute. nil leaves the
// request to gopls.
func (p *proxy) jsxRequest(method string, params map[string]any, vf *vfile, genPath string, off int) func() any {
	src := vf.text()
	switch method {
	case "textDocument/completion":
		if spot, ok := completionSpot(src, off); ok {
			return func() any { return p.completeJSX(vf, genPath, spot, off) }
		}
		ctx, _ := params["context"].(map[string]any)
		if ch, _ := ctx["triggerCharacter"].(string); jsxTriggers[ch] {
			return func() any { return map[string]any{"isIncomplete": false, "items": []any{}} }
		}
	case "textDocument/hover", "textDocument/definition", "textDocument/declaration":
		tag, a, ok := attrAt(src, off)
		if !ok {
			return nil
		}
		return func() any {
			uri, r, ok := p.attrTarget(vf, genPath, tag, a.name)
			if !ok {
				return nil
			}
			if method != "textDocument/hover" {
				return p.rewrite([]any{map[string]any{"uri": uri, "range": r}}, nil, vf)
			}
			res, ok := p.ask(method, map[string]any{"textDocument": map[string]any{"uri": uri}, "position": r.Start})
			var h map[string]any
			if !ok || json.Unmarshal(res, &h) != nil || h == nil {
				return nil
			}
			h["range"] = lspRange{positionOf(src, a.start), positionOf(src, a.end)}
			return staticHover(demangleStrings(h))
		}
	}
	return nil
}

// jsxTriggers are the completion triggers vuka adds for markup; elsewhere
// they complete nothing.
var jsxTriggers = map[string]bool{"<": true, "/": true, " ": true}

// closeRedirect is, for a request on a closing tag's name, the matching place
// on its opening tag's name, which is the one in the generated Go.
func closeRedirect(src []byte, off int) (int, lspRange, bool) {
	c, ok := closeTagAt(src, off)
	if !ok || !isComponentTag(c.name) {
		return off, lspRange{}, false
	}
	o, ok := matchOpen(src, c)
	if !ok {
		return off, lspRange{}, false
	}
	return o.nameStart + off - c.nameStart, lspRange{positionOf(src, c.nameStart), positionOf(src, c.nameEnd)}, true
}

// withCloseTwins adds, to a rename's edits or a list of references or
// highlights, the closing tags of the opening tags they cover.
func (p *proxy) withCloseTwins(method string, v any, origin *vfile) any {
	texts := map[string][]byte{}
	p.mu.Lock()
	var files []*vfile
	for _, vf := range p.virtual {
		if !vf.isTempl() {
			files = append(files, vf)
		}
	}
	p.mu.Unlock()
	for _, vf := range files {
		texts[pathToURI(p.display(vf.source))] = vf.text()
	}
	twins := func(src []byte, list []any) []any {
		if src == nil {
			return list
		}
		for _, e := range list {
			m, _ := e.(map[string]any)
			r, ok := toRange(m["range"])
			if !ok {
				continue
			}
			if t, ok := closeTwin(src, r); ok {
				twin := map[string]any{}
				for k, val := range m {
					twin[k] = val
				}
				twin["range"] = t
				list = append(list, twin)
			}
		}
		return list
	}
	switch method {
	case "textDocument/documentHighlight":
		list, _ := v.([]any)
		return twins(origin.text(), list)
	case "textDocument/references":
		list, _ := v.([]any)
		var out []any
		for _, l := range list {
			out = append(out, l)
			m, _ := l.(map[string]any)
			uri, _ := m["uri"].(string)
			if src := texts[uri]; src != nil {
				if r, ok := toRange(m["range"]); ok {
					if t, ok := closeTwin(src, r); ok {
						out = append(out, map[string]any{"uri": uri, "range": t})
					}
				}
			}
		}
		if out == nil {
			return v
		}
		return out
	case "textDocument/rename":
		we, _ := v.(map[string]any)
		if dc, ok := we["documentChanges"].([]any); ok {
			for _, c := range dc {
				m, _ := c.(map[string]any)
				td, _ := m["textDocument"].(map[string]any)
				uri, _ := td["uri"].(string)
				if edits, ok := m["edits"].([]any); ok {
					m["edits"] = twins(texts[uri], edits)
				}
			}
		}
		if ch, ok := we["changes"].(map[string]any); ok {
			for uri, edits := range ch {
				if list, ok := edits.([]any); ok {
					ch[uri] = twins(texts[uri], list)
				}
			}
		}
	}
	return v
}
