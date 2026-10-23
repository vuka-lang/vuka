package main

import (
	"bytes"
	"go/scanner"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// A .vuka.html file read for what its names are: the header struct's fields
// (props, server state, browser state), its methods, and in the markup every
// name in a hole, a statement or an on… attribute classified by what it
// reads — a field of the struct, an event it sends, a local a statement
// declared — and the tags naming components. Semantic tokens and hover come
// from it, from the editor's text alone, so they keep up while it is typed.

// refKind is what a name in a shape file reads.
type refKind int

const (
	refNone refKind = iota
	refProp
	refState
	refBrowser
	refEvent
	refComponent
	refLocal
)

func (k refKind) String() string {
	switch k {
	case refProp:
		return "prop"
	case refState:
		return "server state"
	case refBrowser:
		return "browser state"
	case refEvent:
		return "event"
	case refComponent:
		return "component"
	case refLocal:
		return "local"
	}
	return ""
}

// shapeField is a named field of the shape's struct.
type shapeField struct {
	name, typ string
	kind      refKind
	off       int // the name, in the header
}

// shapeMethod is a method of the shape's struct, declared in its header.
type shapeMethod struct {
	name string
	off  int    // the name
	hook string // Mount, OnChange or Loads when a hook attribute marks it
	body [2]int // its { and }
}

// shapeLocal is a name a markup statement declares.
type shapeLocal struct {
	name  string
	off   int // where it is declared
	end   int // where its scope ends
	stmt  string
	slot  string // a slot's let: the slot's tag
	comp  string // and the component tag it is in
	index int    // the value of the slot's function it names
}

// shapeRef is a classified name.
type shapeRef struct {
	start, end int
	kind       refKind
	name       string
	decl       bool
	local      *shapeLocal
	event      *eventAttr // the on… attribute it is in
}

// eventAttr is an on… attribute of an element or component.
type eventAttr struct {
	tag  jsxTag
	attr jsxAttrAt
}

type shapeScan struct {
	src     []byte
	shape   *transpile.Shape
	markup  int // where the markup starts; -1 without one
	fields  map[string]*shapeField
	methods map[string]*shapeMethod
	setters map[string][]string // field → the methods (and on… attributes) setting it
	locals  []*shapeLocal
	refs    []shapeRef
	tags    []jsxTag // the tags naming components, opening and closing
	events  []eventAttr
	wired   map[string]bool // the methods an on… attribute sends
}

var (
	hookAttr     = regexp.MustCompile(`^\s*@\s*\w+\s*\.\s*(Mount|OnChange|Loads)\b`)
	stmtHead     = regexp.MustCompile(`^\s*(?:\}\s*)?(?:else\s+)?(for|if|switch)\b(.*)$`)
	assignLocals = regexp.MustCompile(`^\s*([A-Za-z_]\w*(?:\s*,\s*[A-Za-z_]\w*)*)\s*:=`)
)

// scanShape reads src, a .vuka.html file.
func scanShape(src []byte) *shapeScan {
	s := &shapeScan{src: src, markup: -1, fields: map[string]*shapeField{}, methods: map[string]*shapeMethod{},
		setters: map[string][]string{}, wired: map[string]bool{}}
	lines := bytes.SplitAfter(src, []byte("\n"))
	lineOff := make([]int, len(lines)+1)
	for i, l := range lines {
		lineOff[i+1] = lineOff[i] + len(l)
	}
	sh, header, markup := shapeBounds(src)
	s.markup = markup
	if sh != nil {
		s.shape = sh
		for _, f := range sh.Fields {
			if f.Role == transpile.Embedded || f.Line < 1 || f.Line > len(lines) {
				continue
			}
			kind := map[transpile.FieldRole]refKind{transpile.Prop: refProp, transpile.ServerState: refState, transpile.BrowserState: refBrowser}[f.Role]
			off := wordIn(lines[f.Line-1], f.Name)
			if off < 0 {
				continue
			}
			sf := &shapeField{name: f.Name, typ: f.Type, kind: kind, off: lineOff[f.Line-1] + off}
			s.fields[f.Name] = sf
			s.refs = append(s.refs, shapeRef{start: sf.off, end: sf.off + len(f.Name), kind: kind, name: f.Name, decl: true})
		}
		s.headerMethods(header)
	}
	if s.markup >= 0 {
		s.scanMarkup()
	}
	// An exported method an on… attribute sends is an event where it is
	// declared too.
	for name := range s.wired {
		if m := s.methods[name]; m != nil {
			s.refs = append(s.refs, shapeRef{start: m.off, end: m.off + len(name), kind: refEvent, name: name, decl: true})
		}
	}
	slices.SortFunc(s.refs, func(a, b shapeRef) int { return a.start - b.start })
	return s
}

// shapeBounds is where a .vuka.html file's Go header and its markup start,
// as offsets, and the header's struct; sh is nil for a file with no struct
// (its markup starts at the first line starting with <), markup -1 for none.
// Every reader of the boundary in vuka lsp goes through it (splitShape in
// lsp_shapesymbols.go too), and it reads the boundary as the compiler does,
// in the <script lang="go"> form and the Go-header one
// (transpile.SplitTemplate).
func shapeBounds(src []byte) (sh *transpile.Shape, header, markup int) {
	parts, err := transpile.SplitTemplate(src)
	if err != nil {
		return nil, -1, -1 // a <script lang="go"> being typed: Go to the end
	}
	lines := bytes.SplitAfter(src, []byte("\n"))
	lineOff := make([]int, len(lines)+1)
	for i, l := range lines {
		lineOff[i+1] = lineOff[i] + len(l)
	}
	if sh, _ := transpile.ParseShape(src); sh != nil && parts.Markup >= 0 {
		return sh, lineOff[sh.Header], parts.Markup
	}
	if parts.IsScript() {
		return nil, -1, parts.Markup
	}
	for i, l := range lines {
		if len(l) > 0 && l[0] == '<' {
			return nil, -1, lineOff[i]
		}
	}
	return nil, -1, -1
}

// wordIn is the offset of name as a whole word in line, -1 when it isn't.
func wordIn(line []byte, name string) int {
	for i := 0; ; {
		j := bytes.Index(line[i:], []byte(name))
		if j < 0 {
			return -1
		}
		at := i + j
		if (at == 0 || !isIdentByte(line[at-1])) && (at+len(name) >= len(line) || !isIdentByte(line[at+len(name)])) {
			return at
		}
		i = at + 1
	}
}

// headerMethods finds the struct's methods in the header, which hook each
// is, and the fields each sets.
func (s *shapeScan) headerMethods(header int) {
	src := s.src
	method := regexp.MustCompile(`(?m)^func\s*\(\s*(\w+)?\s*\*?\s*` + regexp.QuoteMeta(s.shape.Type) + `\b[^)]*\)\s*(\w+)`)
	head := src[:s.markup]
	for _, m := range method.FindAllSubmatchIndex(head, -1) {
		if m[0] < header {
			continue
		}
		sm := &shapeMethod{name: string(head[m[4]:m[5]]), off: m[4], body: [2]int{-1, -1}}
		// The hook attribute on the lines above.
		for l := lineBefore(src, m[0]); l >= header; l = lineBefore(src, l) {
			line := lineAt(src, l)
			if t := strings.TrimSpace(string(line)); t == "" || strings.HasPrefix(t, "//") {
				continue
			}
			if h := hookAttr.FindSubmatch(line); h != nil {
				sm.hook = string(h[1])
				continue
			}
			if !strings.HasPrefix(strings.TrimSpace(string(line)), "@") {
				break
			}
		}
		if open := bodyOpen(src, m[1], s.markup); open >= 0 {
			if e := skipGo(src, open); e > 0 && e <= s.markup {
				sm.body = [2]int{open, e - 1}
			}
		}
		s.methods[sm.name] = sm
		if sm.body[0] >= 0 {
			recv := ""
			if m[2] >= 0 {
				recv = string(head[m[2]:m[3]])
			}
			s.headerBody(sm, recv)
		}
	}
}

// lineBefore is the start of the line before the one holding off, -1 at the first.
func lineBefore(src []byte, off int) int {
	start := bytes.LastIndexByte(src[:off], '\n') + 1
	if start == 0 {
		return -1
	}
	return bytes.LastIndexByte(src[:start-1], '\n') + 1
}

// lineAt is the line starting at off, without its newline.
func lineAt(src []byte, off int) []byte {
	if e := bytes.IndexByte(src[off:], '\n'); e >= 0 {
		return src[off : off+e]
	}
	return src[off:]
}

// bodyOpen is the { opening the body of a func whose signature goes on from
// i: the first outside the parentheses of its parameters and results.
func bodyOpen(src []byte, i, limit int) int {
	depth := 0
	for ; i < limit; i++ {
		switch src[i] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case '{':
			if depth == 0 {
				// struct{} and interface{} in a signature are types, not the body.
				before := strings.TrimRight(string(src[max(0, i-10):i]), " \t")
				if strings.HasSuffix(before, "struct") || strings.HasSuffix(before, "interface") {
					if e := skipGo(src, i); e > 0 {
						i = e - 1
						continue
					}
				}
				return i
			}
		case '\n':
			if depth == 0 {
				return -1
			}
		}
	}
	return -1
}

// headerBody classifies recv.Field in a method's body and records the
// fields it assigns.
func (s *shapeScan) headerBody(m *shapeMethod, recv string) {
	if recv == "" || recv == "_" {
		return
	}
	s.goTokens(m.body[0]+1, m.body[1], func(toks []goTok) {
		for i := 2; i < len(toks); i++ {
			t := toks[i]
			if t.tok != token.IDENT || toks[i-1].tok != token.PERIOD || toks[i-2].tok != token.IDENT || toks[i-2].lit != recv {
				continue
			}
			if i >= 3 && toks[i-3].tok == token.PERIOD {
				continue
			}
			f := s.fields[t.lit]
			if f == nil {
				continue
			}
			s.refs = append(s.refs, shapeRef{start: t.off, end: t.off + len(t.lit), kind: f.kind, name: t.lit})
			if assigns(toks, i+1) {
				s.addSetter(t.lit, m.name)
			}
		}
	})
}

// assigns reports whether the tokens from i assign to what comes before
// them: an index or field selector may come between.
func assigns(toks []goTok, i int) bool {
	depth := 0
	for ; i < len(toks); i++ {
		switch t := toks[i].tok; {
		case t == token.LBRACK:
			depth++
		case t == token.RBRACK:
			depth--
		case depth > 0:
		case t == token.PERIOD || t == token.IDENT && i > 0 && toks[i-1].tok == token.PERIOD:
		case t == token.ASSIGN || t == token.INC || t == token.DEC || t >= token.ADD_ASSIGN && t <= token.AND_NOT_ASSIGN:
			return true
		default:
			return false
		}
	}
	return false
}

func (s *shapeScan) addSetter(field, by string) {
	if !slices.Contains(s.setters[field], by) {
		s.setters[field] = append(s.setters[field], by)
	}
}

// goTok is a token of Go in a shape file.
type goTok struct {
	off int
	tok token.Token
	lit string
}

// goTokens scans src[from:to] as Go, tolerantly, and hands its tokens to f.
func (s *shapeScan) goTokens(from, to int, f func([]goTok)) {
	if from >= to || to > len(s.src) {
		return
	}
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, to-from)
	var sc scanner.Scanner
	sc.Init(file, s.src[from:to], func(token.Position, string) {}, 0)
	var toks []goTok
	for {
		pos, tok, lit := sc.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.SEMICOLON && lit == "\n" {
			continue
		}
		toks = append(toks, goTok{from + file.Offset(pos), tok, lit})
	}
	f(toks)
}

// scope is a block of markup statements.
type scope struct {
	locals []*shapeLocal
}

// scanMarkup walks the markup: statements at line starts, elements and
// components with their attributes, and holes.
func (s *shapeScan) scanMarkup() {
	src := s.src
	scopes := []*scope{{}}
	var comps []string    // the component tags open
	var slots []slotScope // the slots with a let open
	lineStart := true
	for i := s.markup; i < len(src); {
		c := src[i]
		if lineStart {
			le := bytes.IndexByte(src[i:], '\n')
			if le < 0 {
				le = len(src) - i
			}
			if templateStmt.Match(src[i : i+le]) {
				scopes = s.statement(i, i+le, scopes)
				i += le
				continue
			}
		}
		switch {
		case c == '\n':
			lineStart = true
			i++
			continue
		case c == ' ' || c == '\t' || c == '\r':
			i++
			continue
		case c == '<':
			t, ok := markupTag(src, i)
			if !ok || t.name == "" && t.close {
				i++
				break
			}
			switch {
			case strings.HasPrefix(t.name, "slot:") && !t.close && !t.self:
				if sc := s.letScope(t, comps); sc != nil {
					scopes = append(scopes, sc)
					slots = append(slots, slotScope{t.name, len(scopes)})
				}
			case strings.HasPrefix(t.name, "slot:") && t.close:
				if n := len(slots); n > 0 && slots[n-1].tag == t.name && len(scopes) >= slots[n-1].depth {
					for _, sc := range scopes[slots[n-1].depth-1:] {
						for _, l := range sc.locals {
							if l.end == 0 {
								l.end = t.start
							}
						}
					}
					scopes, slots = scopes[:slots[n-1].depth-1], slots[:n-1]
				}
			case isComponentTag(t.name) && !strings.Contains(t.name, ":") && t.close:
				for k := len(comps) - 1; k >= 0; k-- {
					if baseName(comps[k]) == baseName(t.name) {
						comps = comps[:k]
						break
					}
				}
			case isComponentTag(t.name) && !strings.Contains(t.name, ":") && !t.self:
				comps = append(comps, t.name)
			}
			s.tag(t, scopes)
			end := t.end
			if !t.done {
				end = max(t.stop, i+1)
			}
			if !t.close && !t.self && (t.name == "script" || t.name == "style") {
				if e := bytes.Index(src[end:], []byte("</"+t.name)); e >= 0 {
					end += e
				} else {
					end = len(src)
				}
			}
			i = end
		case c == '{':
			e := skipGo(src, i)
			if e < 0 {
				e = len(src) + 1
			}
			s.hole(i+1, e-1, nil, scopes)
			i = e
		default:
			i++
		}
		lineStart = false
	}
	for _, sc := range scopes {
		for _, l := range sc.locals {
			if l.end == 0 {
				l.end = len(src)
			}
		}
	}
}

// slotScope is a slot with a let, open: its tag and the depth of the scope
// its let declares.
type slotScope struct {
	tag   string
	depth int
}

// letScope is the scope of a slot's let={t, i}: the names it declares, for
// the slot's content. nil without a let.
func (s *shapeScan) letScope(t jsxTag, comps []string) *scope {
	for _, a := range t.attrs {
		if a.name != "let" || !a.expr || a.valEnd <= a.valStart+1 {
			continue
		}
		comp := ""
		if len(comps) > 0 {
			comp = comps[len(comps)-1]
		}
		sc := &scope{}
		at := a.valStart + 1
		text := string(s.src[at : a.valEnd-1])
		for i, part := range strings.Split(text, ",") {
			n := strings.TrimSpace(part)
			if isIdentName(n) && n != "_" {
				off := at + strings.Index(part, n)
				l := &shapeLocal{name: n, off: off, stmt: "let={" + strings.TrimSpace(text) + "} of <" + t.name + ">", slot: t.name, comp: comp, index: i}
				sc.locals = append(sc.locals, l)
				s.locals = append(s.locals, l)
			}
			at += len(part) + 1
		}
		return sc
	}
	return nil
}

// statement reads a markup line that is Go: it closes and opens blocks and
// declares locals.
func (s *shapeScan) statement(from, to int, scopes []*scope) []*scope {
	line := string(s.src[from:to])
	code := line
	if c := strings.Index(code, "//"); c >= 0 {
		code = code[:c]
	}
	trimmed := strings.TrimSpace(code)
	if strings.HasPrefix(trimmed, "}") && len(scopes) > 1 {
		for _, l := range scopes[len(scopes)-1].locals {
			l.end = from
		}
		scopes = scopes[:len(scopes)-1]
	}
	var names string
	opens := strings.HasSuffix(trimmed, "{")
	if m := stmtHead.FindStringSubmatch(code); m != nil {
		rest := m[2]
		if m[1] != "for" {
			if semi := strings.IndexByte(rest, ';'); semi >= 0 {
				rest = rest[:semi]
			} else {
				rest = ""
			}
		}
		if lhs, _, ok := strings.Cut(rest, ":="); ok {
			names = lhs
		}
	} else if m := assignLocals.FindStringSubmatch(code); m != nil {
		names = m[1]
	}
	target := scopes[len(scopes)-1]
	if opens { // a block, or } else {
		target = &scope{}
		scopes = append(scopes, target)
	}
	if names != "" {
		at := 0
		for _, n := range strings.Split(names, ",") {
			n = strings.TrimSpace(n)
			if n == "" || n == "_" || !isIdentName(n) {
				continue
			}
			off := wordIn([]byte(line[at:]), n)
			if off < 0 {
				continue
			}
			l := &shapeLocal{name: n, off: from + at + off, stmt: strings.TrimSpace(strings.TrimSuffix(trimmed, "{"))}
			at += off + len(n)
			target.locals = append(target.locals, l)
			s.locals = append(s.locals, l)
		}
	}
	s.hole(from, to, nil, scopes)
	return scopes
}

func isIdentName(n string) bool {
	for i := 0; i < len(n); i++ {
		if !isIdentByte(n[i]) {
			return false
		}
	}
	return n != "" && !(n[0] >= '0' && n[0] <= '9')
}

// tag reads a tag: a component's name, and its attributes' Go.
func (s *shapeScan) tag(t jsxTag, scopes []*scope) {
	if t.name != "" && isComponentTag(t.name) && !strings.HasPrefix(t.name, "slot:") && !strings.Contains(t.name, ":") {
		s.tags = append(s.tags, t)
		start := t.nameStart + strings.LastIndexByte(t.name, '.') + 1
		s.refs = append(s.refs, shapeRef{start: start, end: t.nameEnd, kind: refComponent, name: t.name})
	}
	if t.close {
		return
	}
	for _, a := range t.attrs {
		if !a.expr || a.valEnd <= a.valStart+1 {
			continue
		}
		var ev *eventAttr
		if isEventAttr(a.name) {
			ev = &eventAttr{tag: t, attr: a}
			s.events = append(s.events, *ev)
		}
		s.hole(a.valStart+1, a.valEnd-1, ev, scopes)
	}
}

// isEventAttr reports whether an attribute's name is an event's: on, then
// an upper-case letter.
func isEventAttr(name string) bool {
	return len(name) > 2 && strings.HasPrefix(name, "on") && name[2] >= 'A' && name[2] <= 'Z'
}

// hole classifies the names in a stretch of Go of the markup.
func (s *shapeScan) hole(from, to int, ev *eventAttr, scopes []*scope) {
	recv := ""
	if s.shape != nil {
		recv = s.shape.Recv
	}
	s.goTokens(from, to, func(toks []goTok) {
		for i, t := range toks {
			if t.tok != token.IDENT {
				continue
			}
			after := i > 0 && toks[i-1].tok == token.PERIOD
			viaRecv := after && i >= 2 && toks[i-2].tok == token.IDENT && toks[i-2].lit == recv && recv != "" &&
				(i < 3 || toks[i-3].tok != token.PERIOD)
			if after && !viaRecv {
				continue
			}
			if !after {
				if l := lookupLocal(scopes, t.lit, t.off); l != nil {
					s.refs = append(s.refs, shapeRef{start: t.off, end: t.off + len(t.lit), kind: refLocal, name: t.lit, decl: l.off == t.off, local: l})
					continue
				}
			}
			if f := s.fields[t.lit]; f != nil {
				s.refs = append(s.refs, shapeRef{start: t.off, end: t.off + len(t.lit), kind: f.kind, name: t.lit, event: ev})
				if ev != nil && f.kind == refBrowser && assigns(toks, i+1) {
					s.addSetter(t.lit, ev.attr.name+" (line "+strconv.Itoa(int(positionOf(s.src, ev.attr.start).Line)+1)+")")
				}
				continue
			}
			if m := s.methods[t.lit]; m != nil && ev != nil && isExported(t.lit) {
				s.wired[t.lit] = true
				s.refs = append(s.refs, shapeRef{start: t.off, end: t.off + len(t.lit), kind: refEvent, name: t.lit, event: ev})
			}
		}
	})
}

// lookupLocal is the local named name in scope at off.
func lookupLocal(scopes []*scope, name string, off int) *shapeLocal {
	for i := len(scopes) - 1; i >= 0; i-- {
		for j := len(scopes[i].locals) - 1; j >= 0; j-- {
			if l := scopes[i].locals[j]; l.name == name && l.off <= off {
				return l
			}
		}
	}
	return nil
}

// refAt is the classified name holding off.
func (s *shapeScan) refAt(off int) (shapeRef, bool) {
	for _, r := range s.refs {
		if off >= r.start && off <= r.end {
			return r, true
		}
	}
	return shapeRef{}, false
}

// eventAt is the on… attribute whose name or value holds off.
func (s *shapeScan) eventAt(off int) (eventAttr, bool) {
	for _, e := range s.events {
		if off >= e.attr.start && off <= e.attr.valEnd {
			return e, true
		}
	}
	return eventAttr{}, false
}
