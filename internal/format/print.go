package format

import (
	"bytes"
	"strconv"
	"strings"
)

// maxWidth is the column markup wraps at, a tab counting 4.
const maxWidth = 80

type printer struct {
	fm  *formatter
	src []byte
}

// atom is a child as laid out: an element, a hole, a block, or one line of text.
type atom struct {
	n    jNode
	text string
}

// gap is what separates two atoms. A soft gap renders the same as a line break
// (React drops whitespace that holds one); a hard one is spaces that render.
type gap struct {
	hard bool
	s    string // hard: the spaces as written; soft: what joins the atoms on one line
	nl   bool   // soft: written as a line break
}

func isBox(a atom) bool {
	switch a.n.(type) {
	case *jElem, *jBlock:
		return true
	}
	return false
}

// items splits children into atoms and the gaps around them: one more gap
// than atoms, the first before the first atom and the last after the last. A
// block's body (body) drops the spaces it starts and ends with.
func (p *printer) items(kids []jNode, body bool) ([]atom, []gap) {
	var atoms []atom
	gaps := []gap{{}}
	space := func(ws string) {
		g := &gaps[len(gaps)-1]
		switch {
		case ws == "":
		case strings.Contains(ws, "\n"):
			g.nl = true
		default:
			*g = gap{hard: true, s: ws}
		}
	}
	add := func(a atom) {
		atoms = append(atoms, a)
		gaps = append(gaps, gap{})
	}
	for _, k := range kids {
		t, ok := k.(*jText)
		if !ok {
			add(atom{n: k})
			continue
		}
		raw := string(p.src[t.start:t.end])
		rest := strings.TrimLeft(raw, " \t\r\n")
		space(raw[:len(raw)-len(rest)])
		if rest == "" {
			continue
		}
		text := strings.TrimRight(rest, " \t\r\n")
		first := true
		for _, line := range strings.Split(text, "\n") {
			if line = strings.Trim(line, " \t\r"); line == "" {
				continue
			}
			if !first {
				gaps[len(gaps)-1] = gap{s: " ", nl: true}
			}
			add(atom{text: line})
			first = false
		}
		space(rest[len(text):])
	}
	if body {
		for _, i := range []int{0, len(gaps) - 1} {
			if gaps[i].hard {
				gaps[i] = gap{}
			}
		}
	}
	return atoms, gaps
}

// width is the column s ends at, starting at col.
func endCol(s string, col int) int {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return textWidth(s[i+1:])
	}
	return col + textWidth(s)
}

func fits(s string, col int) bool { return !strings.Contains(s, "\n") && col+textWidth(s) <= maxWidth }

func closeTag(el *jElem) string { return "</" + el.tag + ">" }

// keepBroken reports whether children written across lines hold an element
// or a block, so stay one per line even if they would fit on one.
func (p *printer) keepBroken(sp span, kids []jNode) bool {
	if !bytes.ContainsRune(p.src[sp.start:sp.end], '\n') {
		return false
	}
	for _, k := range kids {
		if isBox(atom{n: k}) {
			return true
		}
	}
	return false
}

func openName(el *jElem) string { return "<" + el.tag + el.targs }

// elem lays out an element starting at col on a line indented by indent.
func (p *printer) elem(el *jElem, indent string, col int) string {
	open, ok := p.openFlat(el, indent, col)
	ok = ok && (!el.self || fits(open, col))
	if el.self {
		if ok {
			return open
		}
		return p.openBroken(el, indent)
	}
	atoms, gaps := p.items(el.kids, false)
	if ok && !p.keepBroken(el.inner, el.kids) {
		if inner, flat := p.flat(atoms, gaps, indent, endCol(open, col)); flat {
			if s := open + inner + closeTag(el); fits(s, col) {
				return s
			}
		}
	}
	var b strings.Builder
	if ok && fits(open, col) {
		b.WriteString(open)
	} else {
		b.WriteString(p.openBroken(el, indent))
	}
	if len(atoms) == 0 {
		if gaps[0].hard {
			b.WriteString(gaps[0].s)
		}
	} else {
		p.lines(&b, atoms, gaps, indent, endCol(b.String(), col), true)
	}
	b.WriteString(closeTag(el))
	return b.String()
}

func (p *printer) openFlat(el *jElem, indent string, col int) (string, bool) {
	s := openName(el)
	for _, a := range el.attrs {
		v := p.attr(a, indent, endCol(s, col)+1)
		if strings.Contains(v, "\n") {
			return "", false
		}
		s += " " + v
	}
	if el.self {
		return s + " />", true
	}
	return s + ">", true
}

func (p *printer) openBroken(el *jElem, indent string) string {
	s := openName(el)
	in := indent + "\t"
	for _, a := range el.attrs {
		s += "\n" + in + p.attr(a, in, textWidth(in))
	}
	if el.self {
		return s + "\n" + indent + "/>"
	}
	return s + "\n" + indent + ">"
}

func (p *printer) attr(a *jAttr, indent string, col int) string {
	switch a.kind {
	case 'b':
		return a.name
	case 'e':
		return a.name + "={" + p.goExpr(a.expr, indent, col+len(a.name)+2) + "}"
	}
	return a.name + "=" + a.raw
}

// flat lays atoms out on one line, if they all fit on one.
func (p *printer) flat(atoms []atom, gaps []gap, indent string, col int) (string, bool) {
	var b strings.Builder
	for i, a := range atoms {
		b.WriteString(gaps[i].s)
		s := p.atom(a, indent, endCol(b.String(), col), true)
		if strings.Contains(s, "\n") {
			return "", false
		}
		b.WriteString(s)
	}
	b.WriteString(gaps[len(atoms)].s)
	return b.String(), true
}

// lines lays atoms out inside a parent on a line indented by indent: each
// element or block on its own line, and text where it was written; atoms
// joined by spaces that render, or text touching a tag, stay on one line.
// trail ends with a line break back at indent, for the parent's closing.
func (p *printer) lines(b *strings.Builder, atoms []atom, gaps []gap, indent string, col int, trail bool) {
	in, cur := indent+"\t", indent
	start := b.Len()
	for i, a := range atoms {
		at := endCol(b.String()[start:], col)
		switch g := gaps[i]; {
		case g.hard:
			b.WriteString(g.s)
		case i == 0 || g.nl || atoms[i-1].n != nil && a.n != nil && (isBox(atoms[i-1]) || isBox(a)):
			b.WriteString("\n" + in)
			cur, at = in, -1
		default:
			b.WriteString(g.s)
		}
		col := endCol(b.String()[start:], col)
		if at >= 0 && isBox(a) {
			// Sharing a line it can't leave, an element is laid out as it
			// would be on a line of its own.
			col = textWidth(in)
		}
		b.WriteString(p.atom(a, cur, col, false))
	}
	switch g := gaps[len(atoms)]; {
	case g.hard:
		b.WriteString(g.s)
	case trail:
		b.WriteString("\n" + indent)
	}
}

func (p *printer) atom(a atom, indent string, col int, flat bool) string {
	switch n := a.n.(type) {
	case nil:
		return a.text
	case *jElem:
		if flat {
			return p.flatElem(n, indent, col)
		}
		return p.elem(n, indent, col)
	case *jHole:
		if n.verbatim {
			return string(p.src[n.start:n.end])
		}
		return "{" + p.goExpr(n.expr, indent, col+1) + "}"
	case *jBlock:
		if flat {
			s, _ := p.flatBlock(n, indent, col)
			return s
		}
		return p.block(n, indent, col)
	}
	return ""
}

// flatElem is an element on one line; it holds a line break if it has none.
func (p *printer) flatElem(el *jElem, indent string, col int) string {
	open, ok := p.openFlat(el, indent, col)
	if !ok {
		return "\n"
	}
	if el.self {
		return open
	}
	atoms, gaps := p.items(el.kids, false)
	inner, ok := p.flat(atoms, gaps, indent, endCol(open, col))
	if !ok {
		return "\n"
	}
	return open + inner + closeTag(el)
}

func (p *printer) head(b *jBlock, i int, indent string) string {
	if b.heads[i].start < 0 {
		return ""
	}
	return p.fm.header(b.kw, string(p.src[b.heads[i].start:b.heads[i].end]), indent)
}

func (p *printer) branch(b *jBlock, i int, indent string) string {
	switch {
	case i == 0:
		return b.kw + " " + p.head(b, i, indent) + " {"
	case b.heads[i].start < 0:
		return " else {"
	}
	return " else if " + p.head(b, i, indent) + " {"
}

func (p *printer) flatBlock(b *jBlock, indent string, col int) (string, bool) {
	if b.kw == "match" {
		return "\n", false
	}
	s := ""
	if !b.bare {
		s = "{"
	}
	for i, kids := range b.bodies {
		if i > 0 {
			s += "}"
		}
		s += p.branch(b, i, indent)
		atoms, gaps := p.items(kids, true)
		if len(atoms) > 0 {
			inner, ok := p.flat(atoms, gaps, indent, endCol(s, col)+1)
			if !ok {
				return "\n", false
			}
			s += " " + inner + " "
		}
	}
	if s += "}"; !b.bare {
		s += "}"
	}
	return s, !strings.Contains(s, "\n")
}

func (p *printer) block(b *jBlock, indent string, col int) string {
	broken := false
	for _, kids := range b.bodies {
		broken = broken || p.keepBroken(span{b.start, b.end}, kids)
	}
	if s, ok := p.flatBlock(b, indent, col); ok && !broken && fits(s, col) {
		return s
	}
	var w strings.Builder
	close := "}"
	if !b.bare {
		w.WriteString("{")
		close = "}}"
	}
	if b.kw == "match" {
		w.WriteString("match " + p.goExpr(b.subj, indent, col+7) + " {")
		for _, c := range b.cases {
			w.WriteString("\n" + indent)
			if c.def {
				w.WriteString("default:")
			} else {
				var pats []string
				for _, sp := range c.pats {
					pats = append(pats, p.goExpr(sp, indent, textWidth(indent)+5))
				}
				w.WriteString("case " + strings.Join(pats, ", "))
				if c.guard.start >= 0 {
					w.WriteString(" if " + p.goExpr(c.guard, indent, textWidth(indent)+5))
				}
				w.WriteString(":")
			}
			if atoms, gaps := p.items(c.body, true); len(atoms) > 0 {
				p.lines(&w, atoms, gaps, indent, endCol(w.String(), col), false)
			}
		}
		w.WriteString("\n" + indent + close)
		return w.String()
	}
	for i, kids := range b.bodies {
		if i > 0 {
			w.WriteString("}")
		}
		w.WriteString(p.branch(b, i, indent))
		if atoms, gaps := p.items(kids, true); len(atoms) > 0 {
			p.lines(&w, atoms, gaps, indent, endCol(w.String(), col), true)
		}
	}
	w.WriteString(close)
	return w.String()
}

// goExpr is the Go expression at sp formatted as gofmt would, starting at col
// on a line indented by indent, or as written when it isn't Go on its own.
func (p *printer) goExpr(sp span, indent string, col int) string {
	text := string(p.src[sp.start:sp.end])
	s, err := p.fm.expr(text, indent, col)
	if err != nil {
		return text
	}
	return s
}

// expr formats a Go expression (which may hold JSX) as the right side of an
// assignment at the given indentation and column.
func (fm *formatter) expr(text, indent string, col int) (string, error) {
	key := text + "\x00" + indent + "\x00" + strconv.Itoa(col)
	if r, ok := fm.exprs[key]; ok {
		if !r.ok {
			return "", errNotGo
		}
		return r.text, nil
	}
	depth := max(len(indent), 1)
	lhs := "_"
	if n := col - 4*depth - 3; n > 1 {
		lhs = strings.Repeat("_", n)
	}
	prefix := strings.Repeat("\t", depth) + lhs + " = "
	s, err := fm.wrapped(prefix+text, depth)
	if err == nil {
		if i := strings.Index(s, prefix); i >= 0 {
			s = s[i+len(prefix):]
		} else {
			err = errNotGo
		}
	}
	fm.exprs[key] = exprResult{s, err == nil}
	return s, err
}

// header formats the header of a for or an if.
func (fm *formatter) header(kw, text, indent string) string {
	depth := max(len(indent), 1)
	prefix := strings.Repeat("\t", depth) + kw + " "
	s, err := fm.wrapped(prefix+text+" {\n"+strings.Repeat("\t", depth)+"}", depth)
	if err != nil || !strings.HasPrefix(s, prefix) {
		return strings.TrimSpace(text)
	}
	s = s[len(prefix):]
	if i := strings.Index(s, " {\n"); i >= 0 && !strings.Contains(s[:i], "\n") {
		return s[:i]
	}
	return strings.TrimSpace(text)
}

// wrapped formats stmt as a statement depth blocks deep in a function and
// returns its formatted text, indentation included.
func (fm *formatter) wrapped(stmt string, depth int) (string, error) {
	var w strings.Builder
	w.WriteString("package p\n\nfunc _() {\n")
	for i := 1; i < depth; i++ {
		w.WriteString(strings.Repeat("\t", i) + "{\n")
	}
	w.WriteString(stmt + "\n")
	for i := depth - 1; i >= 1; i-- {
		w.WriteString(strings.Repeat("\t", i) + "}\n")
	}
	w.WriteString("}\n")
	out, err := fm.file([]byte(w.String()))
	if err != nil {
		return "", err
	}
	s := string(out)
	start := len("package p\n\nfunc _() {\n")
	for i := 1; i < depth; i++ {
		start += i + 2
	}
	end := len(s)
	for i := 0; i < depth; i++ {
		end = strings.LastIndexByte(s[:end-1], '\n') + 1
	}
	if start > end || !strings.HasPrefix(s, "package p\n\nfunc _() {\n") {
		return "", errNotGo
	}
	return strings.TrimSuffix(s[start:end], "\n"), nil
}
