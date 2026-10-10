package main

import (
	"slices"
	"sort"
	"strings"
)

// The markup of a .vuka file read whole, tolerantly, as it is mid-edit: which
// opening tag each closing tag closes (fragments too), and the {…} inside
// markup. Linked editing, folding, matching highlights and closing-tag
// completion come from it, never from the generated Go.

type markupElem struct {
	open, close jsxTag
	closed      bool
}

type markupScan struct {
	src    []byte
	elems  []markupElem
	braces [][2]int     // a {…} inside markup: the offsets of { and }
	child  map[int]bool // a { opening a child: an expression or a block
}

func scanMarkup(src []byte) *markupScan {
	s := &markupScan{src: src, child: map[int]bool{}}
	s.goCode(0, false, nil)
	return s
}

// markupTag reads the tag at i, a fragment's only when its > follows.
func markupTag(src []byte, i int) (jsxTag, bool) {
	t, ok := readTag(src, i)
	if !ok || t.name == "" && (t.nameEnd >= len(src) || src[t.nameEnd] != '>') {
		return t, false
	}
	return t, true
}

// skipQuoted returns the offset after the string or rune at i; one that
// isn't closed on its line ends there.
func skipQuoted(src []byte, i int) int {
	q := src[i]
	for i++; i < len(src); i++ {
		switch {
		case src[i] == q:
			return i + 1
		case src[i] == '\\' && q != '`':
			i++
		case src[i] == '\n' && q != '`':
			return i
		}
	}
	return i
}

// goCode scans Go from i. Nested (after a { of markup), it stops after the }
// closing it (closed), or at a closing tag of one of the elements anc around
// it (not closed); else at the end.
func (s *markupScan) goCode(i int, nested bool, anc []string) (int, bool) {
	src := s.src
	var opens []int
	for i < len(src) {
		switch c := src[i]; {
		case c == '"' || c == '\'' || c == '`':
			i = skipQuoted(src, i)
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			if e := strings.Index(string(src[i+2:]), "*/"); e >= 0 {
				i += e + 4
			} else {
				i = len(src)
			}
		case c == '{':
			opens = append(opens, i)
			i++
		case c == '}':
			if len(opens) == 0 {
				if nested {
					return i + 1, true
				}
				i++
				continue
			}
			if len(anc) > 0 {
				s.braces = append(s.braces, [2]int{opens[len(opens)-1], i})
			}
			opens = opens[:len(opens)-1]
			i++
		case c == '<' && i+1 < len(src):
			if t, ok := markupTag(src, i); ok && t.close && t.done && slices.Contains(anc, t.name) {
				return i, false
			}
			if n := src[i+1]; (isLetter(n) || n == '>') && markupStarts(src, i) {
				if t, ok := markupTag(src, i); ok && t.done && !t.close {
					if t.self {
						i = t.end
					} else {
						i = s.element(t, anc)
					}
					continue
				}
			}
			i++
		default:
			i++
		}
	}
	return i, false
}

// element scans an element's children from its opening tag; it returns the
// offset after its closing tag, or where an ancestor's closing tag shows it
// unclosed.
func (s *markupScan) element(open jsxTag, anc []string) int {
	src := s.src
	idx := len(s.elems)
	s.elems = append(s.elems, markupElem{open: open})
	outer := anc
	anc = append(anc[:len(anc):len(anc)], open.name)
	i := open.end
	for i < len(src) {
		switch src[i] {
		case '<':
			t, ok := markupTag(src, i)
			switch {
			case !ok:
				i++
			case t.close && t.done:
				if t.name == open.name {
					s.elems[idx].close, s.elems[idx].closed = t, true
					return t.end
				}
				if slices.Contains(outer, t.name) {
					return i
				}
				i = t.end
			case t.done && t.self:
				i = t.end
			case t.done:
				i = s.element(t, anc)
			default:
				i = max(t.stop, i+1)
			}
		case '{':
			s.child[i] = true
			end, closed := s.goCode(i+1, true, anc)
			if closed {
				s.braces = append(s.braces, [2]int{i, end - 1})
			}
			i = end
		default:
			i++
		}
	}
	return i
}

// at is the element whose opening or closing tag's name holds off.
func (s *markupScan) at(off int) (markupElem, bool) {
	for _, e := range s.elems {
		if off >= e.open.nameStart && off <= e.open.nameEnd || e.closed && off >= e.close.nameStart && off <= e.close.nameEnd {
			return e, true
		}
	}
	return markupElem{}, false
}

// unclosedAt is the innermost element still open at off: started before it
// and not closed before it.
func (s *markupScan) unclosedAt(off int) (markupElem, bool) {
	var best markupElem
	found := false
	for _, e := range s.elems {
		if e.open.end <= off && (!e.closed || e.close.start >= off) && (!found || e.open.start > best.open.start) {
			best, found = e, true
		}
	}
	return best, found
}

func (e markupElem) nameRanges(src []byte) []lspRange {
	return []lspRange{
		{positionOf(src, e.open.nameStart), positionOf(src, e.open.nameEnd)},
		{positionOf(src, e.close.nameStart), positionOf(src, e.close.nameEnd)},
	}
}

// linkedEditing is the opening and closing tag names, edited as one, of the
// element whose name holds off.
func linkedEditing(src []byte, off int) any {
	e, ok := scanMarkup(src).at(off)
	if !ok || !e.closed {
		return nil
	}
	return map[string]any{"ranges": e.nameRanges(src), "wordPattern": `[A-Za-z_][\w.:-]*`}
}

// markupFolds are the folding ranges of markup: each element and {…} over
// several lines, its closing line left showing.
func markupFolds(src []byte) map[uint32]uint32 {
	s := scanMarkup(src)
	out := map[uint32]uint32{}
	add := func(start, end int) {
		a, b := positionOf(src, start).Line, positionOf(src, end).Line
		if b >= a+2 && out[a] < b-1 {
			out[a] = b - 1
		}
	}
	for _, e := range s.elems {
		if e.closed {
			add(e.open.start, e.close.start)
		}
	}
	for _, b := range s.braces {
		add(b[0], b[1])
	}
	return out
}

// foldingRanges merges gopls's folding ranges of the generated Go, mapped
// onto the source where they map whole and outside markup, with markup's.
func foldingRanges(vf *vfile, goRanges any) []any {
	src := vf.text()
	folds := markupFolds(src)
	inMarkup := map[uint32]bool{}
	for _, e := range scanMarkup(src).elems {
		end := e.open.end
		if e.closed {
			end = e.close.end
		}
		for l := positionOf(src, e.open.start).Line + 1; l <= positionOf(src, end).Line; l++ {
			inMarkup[l] = true
		}
	}
	list, _ := goRanges.([]any)
	for _, r := range list {
		m, _ := r.(map[string]any)
		sl, ok1 := m["startLine"].(float64)
		el, ok2 := m["endLine"].(float64)
		if !ok1 || !ok2 {
			continue
		}
		gen := lspRange{lspPosition{Line: uint32(sl)}, lspPosition{Line: uint32(el)}}
		got, ok := vf.toSource(gen, true)
		if !ok || got.End.Line <= got.Start.Line || inMarkup[got.Start.Line] || inMarkup[got.End.Line] {
			continue
		}
		if _, dup := folds[got.Start.Line]; !dup {
			folds[got.Start.Line] = got.End.Line
		}
	}
	starts := make([]uint32, 0, len(folds))
	for l := range folds {
		starts = append(starts, l)
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i] < starts[j] })
	out := []any{}
	for _, l := range starts {
		out = append(out, map[string]any{"startLine": l, "endLine": folds[l]})
	}
	return out
}

// htmlTagAt is the tag whose name holds off, when it names an HTML or SVG element.
func htmlTagAt(src []byte, off int) (jsxTag, bool) {
	s := min(off, len(src))
	for s > 0 && isNameByte(src[s-1]) {
		s--
	}
	lt := s - 1
	if lt > 0 && src[lt] == '/' {
		lt--
	}
	if lt < 0 || src[lt] != '<' {
		return jsxTag{}, false
	}
	t, ok := readTag(src, lt)
	return t, ok && t.name != "" && !isComponentTag(t.name) && off <= t.nameEnd
}

// fragmentAt reports whether off is in a fragment's <> or </>.
func fragmentAt(src []byte, off int) bool {
	for _, lt := range []int{off - 1, off - 2} {
		if lt >= 0 && lt+1 < len(src) && src[lt] == '<' {
			if t, ok := markupTag(src, lt); ok && t.name == "" && t.done && off < t.end {
				return true
			}
		}
	}
	return false
}

// htmlAttrAt is the attribute of an HTML or SVG element whose name holds off.
func htmlAttrAt(src []byte, off int) (jsxTag, jsxAttrAt, bool) {
	t, ok := tagAround(src, off)
	if !ok || t.close || isComponentTag(t.name) {
		return t, jsxAttrAt{}, false
	}
	for _, a := range t.attrs {
		if off >= a.start && off <= a.end {
			return t, a, true
		}
	}
	return t, jsxAttrAt{}, false
}

func markdownHover(doc string, src []byte, start, end int) any {
	return map[string]any{"contents": map[string]any{"kind": "markdown", "value": doc},
		"range": lspRange{positionOf(src, start), positionOf(src, end)}}
}

// markupRequest answers, from the source alone, a request on markup gopls
// has nothing for: linked editing of tag names, and hover, highlights and
// navigation on an HTML element's name or attribute. nil leaves the request
// to the rest.
func markupRequest(method string, vf *vfile, off int) func() any {
	src := vf.text()
	if method == "textDocument/linkedEditingRange" {
		return func() any { return linkedEditing(src, off) }
	}
	if fragmentAt(src, off) && method != "textDocument/completion" {
		return func() any {
			if method == "textDocument/hover" {
				return markdownHover("A fragment: its children with no element around them.", src, off, off)
			}
			return nil
		}
	}
	if t, ok := htmlTagAt(src, off); ok {
		switch method {
		case "textDocument/hover":
			return func() any {
				if e := htmlElements[t.name]; e != nil {
					return markdownHover("```html\n<"+t.name+">\n```\n\n"+elementDoc(e), src, t.nameStart, t.nameEnd)
				}
				return nil
			}
		case "textDocument/documentHighlight":
			return func() any {
				e, ok := scanMarkup(src).at(off)
				if !ok || !e.closed {
					return []any{}
				}
				var out []any
				for _, r := range e.nameRanges(src) {
					out = append(out, map[string]any{"range": r, "kind": 1})
				}
				return out
			}
		case "textDocument/completion":
			return nil
		}
		return func() any { return nil }
	}
	if t, a, ok := htmlAttrAt(src, off); ok && method != "textDocument/completion" {
		if method != "textDocument/hover" {
			return func() any { return nil }
		}
		return func() any {
			if doc := attrDoc(t.name, a.name); doc != "" {
				return markdownHover("```html\n"+a.name+"\n```\n\n"+doc, src, a.start, a.end)
			}
			return nil
		}
	}
	return nil
}

// blockSpot is, for a completion right after the { of a child in markup,
// the word typed: for, if and match complete to whole blocks.
func blockSpot(src []byte, off int) (int, bool) {
	s := min(off, len(src))
	for s > 0 && isIdentByte(src[s-1]) {
		s--
	}
	if s == 0 || src[s-1] != '{' || !scanMarkup(src).child[s-1] {
		return 0, false
	}
	w := string(src[s:off])
	for _, kw := range []string{"for", "if", "match"} {
		if strings.HasPrefix(kw, w) {
			return s, true
		}
	}
	return 0, false
}

// blockItems are the snippets of the blocks markup takes in braces; the }
// already typed closes the outer brace.
func blockItems(typed lspRange) []any {
	var out []any
	for i, b := range []struct{ kw, detail, text string }{
		{"for", "for block: markup per item", "for ${1:_}, ${2:it} := range ${3:items} {\n\t$0\n}"},
		{"if", "if block: markup when true", "if ${1:cond} {\n\t$0\n}"},
		{"match", "match block: markup per case", "match ${1:expr} {\ncase ${2:Ok(v)}:\n\t$0\n}"},
	} {
		out = append(out, map[string]any{"label": b.kw, "kind": 15, "detail": b.detail, "sortText": "!" + string(rune('0'+i)),
			"filterText": b.kw, "insertTextFormat": 2, "textEdit": map[string]any{"range": typed, "newText": b.text}})
	}
	return out
}

// withBlockItems adds the block snippets to a completion answer.
func withBlockItems(v any, typed lspRange) any {
	return withItems(v, blockItems(typed))
}
