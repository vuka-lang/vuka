package term

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// ANSI SGR codes.
const (
	sgrBold      = "1"
	sgrDim       = "2"
	sgrItalic    = "3"
	sgrUnderline = "4"
	sgrInverse   = "7"
	sgrStrike    = "9"
)

const (
	nbsp      = ' ' // keeps an atom ("[ OK ]") on one line; written as a space
	ruleWidth = 40  // an hr's width when nothing wraps
)

// line is one output line and its width in columns.
type line struct {
	s string
	w int
}

// seg is a run of inline text in one style (SGR codes joined by ';'), or a
// line break.
type seg struct {
	text  string
	style string
	br    bool
}

type layout struct {
	o     Options
	depth int // list nesting
}

func width(s string) int { return utf8.RuneCountInString(s) }

func narrower(w, by int) int {
	if w == 0 {
		return 0
	}
	return max(w-by, 1)
}

func with(style []string, code string) []string {
	return append(style[:len(style):len(style)], code)
}

func (l *layout) sgr(style []string) string {
	if !l.o.Color {
		return ""
	}
	return strings.Join(style, ";")
}

func (l *layout) styled(s string, style ...string) line {
	if l.o.Color && len(style) > 0 {
		return line{"\x1b[" + strings.Join(style, ";") + "m" + s + "\x1b[0m", width(s)}
	}
	return line{s, width(s)}
}

// blocks lays out a sequence of children: runs of inline content become
// wrapped paragraphs, block elements lay themselves out, and the pieces are
// separated by a blank line (none when tight, as inside a list item).
func (l *layout) blocks(kids []*node, w int, style []string, tight bool) []line {
	var out []line
	add := func(ls []line) {
		if len(ls) == 0 {
			return
		}
		if len(out) > 0 && !tight {
			out = append(out, line{})
		}
		out = append(out, ls...)
	}
	var run []*node
	flush := func() {
		if len(run) > 0 {
			add(l.wrap(l.inline(run, style), w))
			run = nil
		}
	}
	for _, k := range kids {
		if k.tag != "" && isBlock(k.tag) {
			flush()
			if !skipped(k.tag) {
				add(l.block(k, w, style))
			}
			continue
		}
		run = append(run, k)
	}
	flush()
	return out
}

func (l *layout) block(n *node, w int, style []string) []line {
	switch n.tag {
	case "h1", "h2", "h3", "h4", "h5", "h6":
		ls := l.wrap(l.inline(n.kids, with(style, sgrBold)), w)
		rule := map[string]string{"h1": "═", "h2": "─"}[n.tag]
		if rule != "" && len(ls) > 0 {
			rw := 0
			for _, ln := range ls {
				rw = max(rw, ln.w)
			}
			ls = append(ls, l.styled(strings.Repeat(rule, rw)))
		}
		return ls
	case "hr":
		rw := w
		if rw == 0 {
			rw = ruleWidth
		}
		return []line{l.styled(strings.Repeat("─", rw), sgrDim)}
	case "pre":
		text := strings.TrimPrefix(plainText(n), "\n")
		text = strings.TrimRight(text, "\n")
		if text == "" {
			return nil
		}
		var ls []line
		for _, s := range strings.Split(text, "\n") {
			s = "  " + strings.ReplaceAll(s, "\t", "    ")
			ls = append(ls, line{s, width(s)})
		}
		return ls
	case "blockquote":
		bar := l.styled("│ ", sgrDim)
		ls := l.blocks(n.kids, narrower(w, 2), style, false)
		for i, ln := range ls {
			ls[i] = line{bar.s + ln.s, 2 + ln.w}
		}
		return ls
	case "ul", "ol":
		return l.list(n, w, style)
	case "table":
		return l.table(n, style)
	}
	return l.blocks(n.kids, w, style, false)
}

func (l *layout) list(n *node, w int, style []string) []line {
	var items []*node
	for _, k := range n.kids {
		if k.tag == "" && strings.TrimSpace(k.text) == "" {
			continue
		}
		items = append(items, k)
	}
	start := 1
	if s, ok := n.attr("start"); ok {
		if v, err := strconv.Atoi(s); err == nil {
			start = v
		}
	}
	bullet := []string{"•", "◦", "▪"}[l.depth%3]
	digits := len(strconv.Itoa(start + len(items) - 1))
	l.depth++
	defer func() { l.depth-- }()
	var out []line
	for i, it := range items {
		marker := bullet + " "
		if n.tag == "ol" {
			num := strconv.Itoa(start + i)
			marker = strings.Repeat(" ", digits-len(num)) + num + ". "
		}
		mw := width(marker)
		kids := []*node{it}
		if it.tag == "li" {
			kids = it.kids
		}
		ls := l.blocks(kids, narrower(w, mw), style, true)
		if len(ls) == 0 {
			ls = []line{{}}
		}
		for j, ln := range ls {
			prefix := strings.Repeat(" ", mw)
			if j == 0 {
				prefix = marker
			}
			out = append(out, line{prefix + ln.s, mw + ln.w})
		}
	}
	return out
}

// table lays out rows as aligned columns, two spaces apart, with a rule under
// a header row (one in thead, or all th).
func (l *layout) table(n *node, style []string) []line {
	type row struct {
		cells  []line
		header bool
	}
	var rows []row
	var collect func(n *node, head bool)
	collect = func(n *node, head bool) {
		for _, k := range n.kids {
			switch k.tag {
			case "thead":
				collect(k, true)
			case "tbody", "tfoot":
				collect(k, false)
			case "tr":
				r := row{header: head}
				allTh := true
				for _, c := range k.kids {
					if c.tag != "td" && c.tag != "th" {
						continue
					}
					st := style
					if c.tag == "th" {
						st = with(style, sgrBold)
					} else {
						allTh = false
					}
					ls := l.wrap(l.inline(c.kids, st), 0)
					cell := line{}
					for i, ln := range ls {
						if i > 0 {
							cell.s += " "
							cell.w++
						}
						cell.s += ln.s
						cell.w += ln.w
					}
					r.cells = append(r.cells, cell)
				}
				r.header = r.header || (allTh && len(r.cells) > 0)
				rows = append(rows, r)
			}
		}
	}
	collect(n, false)
	var cols []int
	for _, r := range rows {
		for i, c := range r.cells {
			if i == len(cols) {
				cols = append(cols, 0)
			}
			cols[i] = max(cols[i], c.w)
		}
	}
	var out []line
	for i, r := range rows {
		var sb strings.Builder
		w := 0
		for j, c := range r.cells {
			if j > 0 {
				sb.WriteString("  ")
				w += 2
			}
			sb.WriteString(c.s)
			sb.WriteString(strings.Repeat(" ", cols[j]-c.w))
			w += cols[j]
		}
		out = append(out, line{sb.String(), w})
		if r.header && i < len(rows)-1 && !rows[i+1].header {
			parts := make([]string, len(cols))
			for j, cw := range cols {
				parts[j] = strings.Repeat("─", cw)
			}
			out = append(out, l.styled(strings.Join(parts, "  "), sgrDim))
		}
	}
	return out
}

// inline flattens inline content into styled segments.
func (l *layout) inline(nodes []*node, style []string) []seg {
	var out []seg
	for _, n := range nodes {
		l.collect(n, style, &out)
	}
	return out
}

func (l *layout) collect(n *node, style []string, out *[]seg) {
	if n.tag == "" {
		*out = append(*out, seg{text: n.text, style: l.sgr(style)})
		return
	}
	atom := func(s string) {
		*out = append(*out, seg{text: strings.ReplaceAll(s, " ", string(nbsp)), style: l.sgr(style)})
	}
	kids := func(style []string) {
		for _, k := range n.kids {
			l.collect(k, style, out)
		}
	}
	switch n.tag {
	case "b", "strong":
		kids(with(style, sgrBold))
	case "i", "em", "cite", "var", "dfn":
		kids(with(style, sgrItalic))
	case "u", "ins":
		kids(with(style, sgrUnderline))
	case "s", "del", "strike":
		kids(with(style, sgrStrike))
	case "mark":
		kids(with(style, sgrInverse))
	case "code", "kbd", "samp", "tt":
		if l.o.Color {
			kids(with(style, sgrInverse))
		} else {
			*out = append(*out, seg{text: "`"})
			kids(style)
			*out = append(*out, seg{text: "`"})
		}
	case "a":
		if l.o.Color {
			kids(with(style, sgrUnderline))
		} else {
			kids(style)
		}
		if href, _ := n.attr("href"); href != "" && href != collapse(plainText(n)) {
			*out = append(*out, seg{text: " (" + href + ")", style: l.sgr(style)})
		}
	case "br":
		*out = append(*out, seg{br: true})
	case "img":
		alt, _ := n.attr("alt")
		if alt == "" {
			alt = "image"
		}
		atom("[" + collapse(alt) + "]")
	case "button":
		atom("[ " + collapse(plainText(n)) + " ]")
	case "input":
		l.input(n, atom)
	default:
		if skipped(n.tag) {
			return
		}
		if isBlock(n.tag) { // a block inside inline content: keep words apart
			*out = append(*out, seg{text: " "})
			kids(style)
			*out = append(*out, seg{text: " "})
			return
		}
		kids(style)
	}
}

func (l *layout) input(n *node, atom func(string)) {
	typ, _ := n.attr("type")
	_, checked := n.attr("checked")
	value, _ := n.attr("value")
	switch strings.ToLower(typ) {
	case "hidden":
	case "checkbox":
		atom(map[bool]string{true: "[x]", false: "[ ]"}[checked])
	case "radio":
		atom(map[bool]string{true: "(•)", false: "( )"}[checked])
	case "submit", "button", "reset":
		if value == "" {
			value = map[string]string{"reset": "Reset"}[typ]
			if value == "" {
				value = "Submit"
			}
		}
		atom("[ " + value + " ]")
	default:
		if value == "" {
			value, _ = n.attr("placeholder")
		}
		const field = 12
		atom("[" + value + strings.Repeat("_", max(field-width(value), 1)) + "]")
	}
}

// plainText is the text under n, as written.
func plainText(n *node) string {
	if n.tag == "" {
		return n.text
	}
	if skipped(n.tag) {
		return ""
	}
	var sb strings.Builder
	for _, k := range n.kids {
		sb.WriteString(plainText(k))
	}
	return sb.String()
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
}

// word is a run of non-space text, possibly in several styles.
type word struct {
	segs  []seg
	w     int
	space bool // whitespace before it
	br    bool // a line break, not a word
}

// wrap collapses whitespace like a browser and fills lines up to w columns
// (0: one line per explicit break). A word wider than w gets a line of its own.
func (l *layout) wrap(segs []seg, w int) []line {
	var words []word
	in := false // inside the last word
	space := false
	for _, s := range segs {
		if s.br {
			words = append(words, word{br: true})
			in, space = false, false
			continue
		}
		for _, r := range s.text {
			if isSpace(r) {
				in, space = false, true
				continue
			}
			if !in {
				words = append(words, word{space: space})
				in, space = true, false
			}
			cur := &words[len(words)-1]
			if n := len(cur.segs); n > 0 && cur.segs[n-1].style == s.style {
				cur.segs[n-1].text += string(r)
			} else {
				cur.segs = append(cur.segs, seg{text: string(r), style: s.style})
			}
			cur.w++
		}
	}
	var out []line
	var ln []word
	lw := 0
	emit := func() {
		out = append(out, render(ln, lw))
		ln, lw = nil, 0
	}
	for _, wd := range words {
		if wd.br {
			emit()
			continue
		}
		need := wd.w
		if wd.space && len(ln) > 0 {
			need++
		}
		if w > 0 && len(ln) > 0 && lw+need > w {
			emit()
			need = wd.w
		}
		ln = append(ln, wd)
		lw += need
	}
	if len(ln) > 0 {
		emit()
	}
	return out
}

// render writes a line's words, a space taking its neighbours' style when
// they share one, and each run of one style wrapped in its SGR codes.
func render(words []word, w int) line {
	var pieces []seg
	push := func(s seg) {
		if n := len(pieces); n > 0 && pieces[n-1].style == s.style {
			pieces[n-1].text += s.text
			return
		}
		pieces = append(pieces, s)
	}
	for i, wd := range words {
		if i > 0 && wd.space {
			st := ""
			if prev := words[i-1].segs; prev[len(prev)-1].style == wd.segs[0].style {
				st = wd.segs[0].style
			}
			push(seg{text: " ", style: st})
		}
		for _, s := range wd.segs {
			push(s)
		}
	}
	var sb strings.Builder
	for _, p := range pieces {
		text := strings.ReplaceAll(p.text, string(nbsp), " ")
		if p.style == "" {
			sb.WriteString(text)
			continue
		}
		sb.WriteString("\x1b[" + p.style + "m" + text + "\x1b[0m")
	}
	return line{sb.String(), w}
}
