package transpile

import (
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

// A .vuka.html file is Go above markup: its package clause and imports, then
// a header of Go — one struct, the decorated hooks and methods of it, other
// helpers — up to the first line starting with <, then the markup that
// struct renders:
//
//	package pages
//
//	type Index struct {
//		ui.Live
//		rows []Todo
//	}
//
//	@ui.Mount
//	func (p *Index) mount(ctx context.Context) error { … }
//	<ul>…</ul>
//
// The header is .vuka source and compiles as such, without markup (E0151);
// the markup is the struct's Render (a live struct) or View (a static one).
// Nothing follows it: a func after the markup is a second template (E0153).

// Shape is a template file's header: the struct its markup renders.
type Shape struct {
	Header int // the line its Go starts on, from 0
	Markup int // the line its markup starts on, from 0: the first starting with <

	Type     string // the struct it renders
	TypeLine int    // from 1
	Generic  bool   // the struct has type parameters
	TypeArgs string // and their names, as a receiver writes them: [K, V]
	Recv     string // the receiver its methods name, "" when none does
	Ptr      bool   // a method has a pointer receiver
	Fields   []ShapeField

	// Live is whether the struct is a live component: it embeds ui.Live,
	// declares a hook, or has an exported method the markup wires to an
	// on… attribute other than as a Push or Options step's handler, which
	// a page that isn't live calls over HTTP. LiveWhy says which, for
	// messages.
	Live       bool
	LiveWhy    string
	EmbedsLive bool

	open int // the offset just past the struct's {
}

// FieldRole is what a field of a rendered struct is.
type FieldRole int

const (
	// Prop is an exported field: set by a tag's attribute, or on a page
	// from its path or query.
	Prop FieldRole = iota
	// ServerState is an unexported field: state the server keeps.
	ServerState
	// BrowserState is a ui.State[T] field: state the browser keeps.
	BrowserState
	// Embedded is an embedded field (ui.Live, a shared struct).
	Embedded
)

func (r FieldRole) String() string {
	switch r {
	case Prop:
		return "prop"
	case ServerState:
		return "server state"
	case BrowserState:
		return "browser state"
	}
	return "embedded"
}

// ShapeField is a field of the rendered struct.
type ShapeField struct {
	Name string // the embedded type's name for an embedded field
	Type string // as written
	Role FieldRole
	Line int    // from 1
	Tag  string // its struct tag, unquoted: vuka:"required"
}

var (
	shapeStart = regexp.MustCompile(`^(?:type|var|const)\b|^@[A-Za-z_]`)
	shapeType  = regexp.MustCompile(`^type\s+\w+(?:\[[^\]]*\])?\s+struct\b`)
	hookName   = regexp.MustCompile(`^@\s*\w+\s*\.\s*(Mount|OnChange|Loads)\b`)
	eventAttr  = regexp.MustCompile(`\bon[A-Z]\w*=\{`)
)

// isShape reports whether the template file's lines, its head ending at top,
// have a Go header: the first line after the imports that isn't blank or a
// comment starts a declaration (type, var, const) or a decorator, or a struct
// type is declared before the first line starting with <. A file without one
// starts with markup, a statement or a func header: E0152.
func isShape(lines []string, top int) bool {
	first := true
	for i := top; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "//") {
			continue
		}
		if first && shapeStart.MatchString(lines[i]) {
			return true
		}
		first = false
		if strings.HasPrefix(t, "<") {
			return false
		}
		if shapeType.MatchString(lines[i]) {
			return true
		}
	}
	return false
}

// ParseShape reads src, a template file, as its Go header and markup: nil
// without an error when it has no Go header (E0152 when compiled), and an
// error when the header breaks one of its rules.
func ParseShape(src []byte) (*Shape, *Error) {
	src, _, err := headerView(src)
	if err != nil {
		return nil, err
	}
	head := parseTemplateHead(src)
	lines := strings.SplitAfter(string(src), "\n")
	if !isShape(lines, head.Top) {
		return nil, nil
	}
	return parseShape(src, lines, head)
}

func parseShape(src []byte, lines []string, head TemplateHead) (*Shape, *Error) {
	off := make([]int, len(lines)+1)
	for i, l := range lines {
		off[i+1] = off[i] + len(l)
	}
	s := &Shape{Header: head.Top, Markup: -1}
	skel, hooks, markup := goHeader(src, off[head.Top])
	if markup < 0 {
		return nil, &Error{Code: "E0148", Pos: token.Position{Line: head.Top + 1, Column: 1},
			Msg:  "the template file has Go and no markup",
			Help: "write the struct's markup below its Go (below </script>), or make the file a .go file"}
	}
	// The line the markup starts on, its < indented or not.
	for i := range lines {
		if off[i] <= markup && markup < off[i+1] {
			s.Markup = i
		}
	}
	// The head (package clause, imports) and the header's skeleton parse as
	// Go, at the file's own offsets.
	text := string(src[:off[head.Top]]) + skel
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", text, parser.ParseComments)
	if f == nil {
		return nil, shapeSyntax(err)
	}
	var structs []*ast.TypeSpec
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.TYPE {
			continue
		}
		for _, sp := range g.Specs {
			if ts, ok := sp.(*ast.TypeSpec); ok {
				if _, ok := ts.Type.(*ast.StructType); ok {
					structs = append(structs, ts)
				}
			}
		}
	}
	pos := func(p token.Pos) token.Position {
		at := fset.Position(p)
		return token.Position{Line: at.Line, Column: at.Column}
	}
	if len(structs) > 1 {
		// A form the page holds (a struct embedding ui.FormState) may be
		// declared beside it: it is data, never the struct the markup renders.
		var rendered []*ast.TypeSpec
		for _, ts := range structs {
			if !embedsFormState(ts.Type.(*ast.StructType), f) {
				rendered = append(rendered, ts)
			}
		}
		if len(rendered) == 1 {
			structs = rendered
		}
	}
	switch {
	case len(structs) == 0:
		if err != nil {
			return nil, shapeSyntax(err)
		}
		return nil, &Error{Code: "E0146", Pos: token.Position{Line: head.Top + 1, Column: 1},
			Msg:  "the template file's Go declares no struct: a .vuka.html file renders one struct, declared above its markup",
			Help: "declare it: type Page struct { … }"}
	case len(structs) > 1:
		second := structs[1]
		return nil, &Error{Code: "E0147", Pos: pos(second.Name.Pos()), Len: len(second.Name.Name),
			Msg: "the template file declares " + structs[0].Name.Name + " and " + second.Name.Name +
				": a .vuka.html file renders one struct",
			Help: "move " + second.Name.Name + " to its own .vuka.html file, or to a .go file beside this one when it has no markup (a form, a struct embedding ui.FormState, may stay)"}
	}
	// A syntax error past the struct is the compiler's to report, at its
	// place, as for any .vuka source.
	ts := structs[0]
	st := ts.Type.(*ast.StructType)
	s.Type, s.TypeLine, s.Generic = ts.Name.Name, pos(ts.Name.Pos()).Line, ts.TypeParams != nil
	if s.Generic {
		var names []string
		for _, p := range ts.TypeParams.List {
			for _, n := range p.Names {
				names = append(names, n.Name)
			}
		}
		s.TypeArgs = "[" + strings.Join(names, ", ") + "]"
	}
	s.open = fset.Position(st.Fields.Opening).Offset + 1
	for _, fl := range st.Fields.List {
		typ := string(src[fset.Position(fl.Type.Pos()).Offset:fset.Position(fl.Type.End()).Offset])
		if len(fl.Names) == 0 {
			name := shapeEmbedded(fl.Type)
			if name == "Live" {
				s.EmbedsLive = true
			}
			s.Fields = append(s.Fields, ShapeField{Name: name, Type: typ, Role: Embedded, Line: pos(fl.Pos()).Line})
			continue
		}
		tag := ""
		if fl.Tag != nil {
			tag, _ = strconv.Unquote(fl.Tag.Value)
		}
		for _, n := range fl.Names {
			role := ServerState
			switch {
			case isUIState(fl.Type):
				role = BrowserState
			case n.IsExported():
				role = Prop
			}
			s.Fields = append(s.Fields, ShapeField{Name: n.Name, Type: typ, Role: role, Line: pos(n.Pos()).Line, Tag: tag})
			if role == BrowserState {
				s.Ptr = true
			}
		}
	}
	var events []string
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 {
			continue
		}
		r := fd.Recv.List[0]
		ptr, base := shapeRecv(r.Type)
		if base != s.Type {
			continue
		}
		if ptr {
			s.Ptr = true
		}
		if s.Recv == "" && len(r.Names) == 1 && r.Names[0].Name != "_" {
			s.Recv = r.Names[0].Name
		}
		if fd.Name.IsExported() {
			events = append(events, fd.Name.Name)
		}
	}
	switch {
	case s.EmbedsLive:
		s.Live, s.LiveWhy = true, "it embeds ui.Live"
	case len(hooks) > 0:
		s.Live, s.LiveWhy = true, "it declares the hook "+hooks[0]
	default:
		if m := wiredEvent(string(src[markup:]), events); m != "" {
			s.Live, s.LiveWhy = true, "the markup sends the event "+m
		}
	}
	if s.Live {
		s.Ptr = true
	}
	return s, nil
}

// shapeSyntax is the error of a header that doesn't parse.
func shapeSyntax(err error) *Error {
	e := &Error{Code: "E0003", Pos: token.Position{Line: 1, Column: 1}, Msg: "the template file's Go doesn't parse"}
	if l, ok := err.(scanner.ErrorList); ok && len(l) > 0 {
		e.Pos, e.Msg = token.Position{Line: l[0].Pos.Line, Column: l[0].Pos.Column}, l[0].Msg
	}
	return e
}

func shapeEmbedded(x ast.Expr) string {
	switch x := x.(type) {
	case *ast.StarExpr:
		return shapeEmbedded(x.X)
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.Ident:
		return x.Name
	case *ast.IndexExpr:
		return shapeEmbedded(x.X)
	case *ast.IndexListExpr:
		return shapeEmbedded(x.X)
	}
	return ""
}

// isUIState reports whether x is State[T] of a package, ui.State[string].
func isUIState(x ast.Expr) bool {
	ix, ok := x.(*ast.IndexExpr)
	if !ok {
		return false
	}
	switch t := ix.X.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name == "State"
	case *ast.Ident:
		return t.Name == "State"
	}
	return false
}

func shapeRecv(x ast.Expr) (ptr bool, name string) {
	if s, ok := x.(*ast.StarExpr); ok {
		ptr, x = true, s.X
	}
	switch t := x.(type) {
	case *ast.IndexExpr:
		x = t.X
	case *ast.IndexListExpr:
		x = t.X
	}
	if id, ok := x.(*ast.Ident); ok {
		return ptr, id.Name
	}
	return ptr, ""
}

// wiredEvent is the first of the methods events an on… attribute of the
// markup names, "" when none does.
// SendsEvent reports whether a template file's markup wires its struct's
// method name to an on… attribute: sends it as an event.
func SendsEvent(src []byte, name string) bool {
	_, _, markup := goHeader(HeaderView(src), 0)
	return markup >= 0 && wiredEvent(string(src[markup:]), []string{name}) != ""
}

func wiredEvent(markup string, events []string) string {
	if len(events) == 0 {
		return ""
	}
	names := make([]string, len(events))
	for i, e := range events {
		names[i] = regexp.QuoteMeta(e)
	}
	name := regexp.MustCompile(`(?:^|[^\w.]|\w\.)(` + strings.Join(names, "|") + `)\b`)
	for _, loc := range eventAttr.FindAllStringIndex(markup, -1) {
		end := holeEnd(markup, loc[1])
		if end < 0 {
			continue
		}
		if m := name.FindStringSubmatch(maskPushSteps(markup[loc[1]:end])); m != nil {
			return m[1]
		}
	}
	return ""
}

var pushStep = regexp.MustCompile(`\.(?:Push|Options)\(`)

// maskPushSteps is an on… attribute's expression without its Push and
// Options steps' arguments: a method a step names doesn't make its page
// live, the step reaches it over HTTP on a page that isn't. A closure among
// them does, running only on a live instance.
func maskPushSteps(hole string) string {
	for from := 0; ; {
		loc := pushStep.FindStringIndex(hole[from:])
		if loc == nil {
			return hole
		}
		open := from + loc[1]
		if strings.Contains(closeParen(hole, open), "func(") {
			from = open
			continue
		}
		end := open + len(closeParen(hole, open))
		hole = hole[:open] + hole[end:]
		from = open
	}
}

// closeParen is s from i up to the paren closing the one before i.
func closeParen(s string, i int) string {
	depth := 0
	for j := i; j < len(s); j++ {
		switch c := s[j]; c {
		case '"', '\'', '`':
			for j++; j < len(s) && s[j] != c; j++ {
				if s[j] == '\\' && c != '`' {
					j++
				}
			}
		case '(':
			depth++
		case ')':
			if depth == 0 {
				return s[i:j]
			}
			depth--
		}
	}
	return s[i:]
}

// goHeader reads the header's Go, starting at the offset from, up to the
// first line starting with < outside any brace, paren or bracket. It
// returns the header as a skeleton Go parses — the same length, its lines
// kept, with decorators, field attributes and function bodies written as
// spaces (they are .vuka: @, ?, match) —, the hooks its decorators name, and
// the offset its markup starts at, -1 when nothing starts it.
func goHeader(src []byte, from int) (skel string, hooks []string, markup int) {
	markup = -1
	text := src[from:]
	out := []byte(string(text))
	blankRange := func(a, b int) {
		for i := max(a, 0); i < b && i < len(out); i++ {
			if out[i] != '\n' && out[i] != '\r' {
				out[i] = ' '
			}
		}
	}
	// The markup: a line whose first token, at depth 0, is a <.
	lineStart := func(o int) bool {
		for i := o - 1; i >= 0; i-- {
			switch text[i] {
			case ' ', '\t':
				continue
			case '\n':
				return true
			}
			return false
		}
		return true
	}
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(text))
	var sc scanner.Scanner
	sc.Init(file, text, func(token.Position, string) {}, scanner.ScanComments)
	type tok struct {
		t   token.Token
		off int
		lit string
	}
	var toks []tok
	depth := 0
	for {
		p, t, lit := sc.Scan()
		if t == token.EOF {
			break
		}
		o := file.Offset(p)
		if depth == 0 && (t == token.LSS || t == token.SHL || t == token.LEQ) && lineStart(o) {
			markup = from + o
			// Everything from the markup's line on is not the header.
			ls := o
			for ls > 0 && text[ls-1] != '\n' {
				ls--
			}
			out = out[:ls]
			break
		}
		switch t {
		case token.LBRACE, token.LPAREN, token.LBRACK:
			depth++
		case token.RBRACE, token.RPAREN, token.RBRACK:
			depth--
		}
		if t == token.SEMICOLON && lit == "\n" {
			continue
		}
		toks = append(toks, tok{t, o, lit})
	}
	// match is the index of the token closing the group toks[i] opens.
	match := func(i int) int {
		d := 0
		for j := i; j < len(toks); j++ {
			switch toks[j].t {
			case token.LBRACE, token.LPAREN, token.LBRACK:
				d++
			case token.RBRACE, token.RPAREN, token.RBRACK:
				if d--; d == 0 {
					return j
				}
			}
		}
		return len(toks) - 1
	}
	end := func(j int) int { // the offset after toks[j]
		if j >= len(toks) {
			return len(out)
		}
		t := toks[j]
		if t.lit != "" {
			return t.off + len(t.lit)
		}
		return t.off + len(t.t.String())
	}
	depth = 0
	inFunc := false
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch {
		case t.t == token.ILLEGAL && t.lit == "@":
			// A decorator or a field attribute: @pkg.Name, then (…) or {…}.
			j := i + 1
			for j < len(toks) && (toks[j].t == token.IDENT || toks[j].t == token.PERIOD) {
				j++
			}
			if depth == 0 {
				name := strings.Builder{}
				name.WriteString("@")
				for k := i + 1; k < j; k++ {
					name.WriteString(toks[k].lit)
					if toks[k].t == token.PERIOD {
						name.WriteString(".")
					}
				}
				if hookName.MatchString(name.String()) {
					hooks = append(hooks, name.String())
				}
			}
			last := j - 1
			if j < len(toks) && (toks[j].t == token.LPAREN || toks[j].t == token.LBRACE) && toks[j].off == end(j-1) {
				last = match(j)
			}
			// A call chained on it, @ui.URL("page").Default(1), is part of it.
			for k := last + 1; k+1 < len(toks) && toks[k].t == token.PERIOD && toks[k].off == end(last) && toks[k+1].t == token.IDENT; k = last + 1 {
				last = k + 1
				if n := last + 1; n < len(toks) && (toks[n].t == token.LPAREN || toks[n].t == token.LBRACE) && toks[n].off == end(last) {
					last = match(n)
				}
			}
			blankRange(t.off, end(last))
			i = last
		case t.t == token.FUNC && depth == 0:
			inFunc = true
		case t.t == token.LBRACE && inFunc && depth == 0 && i > 0 && toks[i-1].t != token.STRUCT && toks[i-1].t != token.INTERFACE:
			// A function's body: kept as {}, its inside spaces.
			j := match(i)
			blankRange(end(i), toks[j].off)
			inFunc = false
			i = j
		case t.t == token.LBRACE || t.t == token.LPAREN || t.t == token.LBRACK:
			depth++
		case t.t == token.RBRACE || t.t == token.RPAREN || t.t == token.RBRACK:
			depth--
		case t.t == token.TYPE || t.t == token.VAR || t.t == token.CONST:
			if depth == 0 {
				inFunc = false
			}
		}
	}
	return string(out), hooks, markup
}

// templatesTop is the line, from 0, a template file's markup starts on: past
// its head and its Go header (its line count when it has no markup).
func templatesTop(src []byte) int {
	head := ParseTemplateHead(src)
	lines := strings.SplitAfter(string(src), "\n")
	if !isShape(lines, head.Top) {
		return head.Top
	}
	at := 0
	for _, l := range lines[:head.Top] {
		at += len(l)
	}
	_, _, markup := goHeader(src, at)
	if markup < 0 {
		return len(lines)
	}
	return strings.Count(string(src[:markup]), "\n")
}

// hasRole reports whether a field of the struct has role r.
func (s *Shape) hasRole(r FieldRole) bool {
	for _, f := range s.Fields {
		if f.Role == r {
			return true
		}
	}
	return false
}

// statesCall stands, in a static shape struct's View, for the gen package's
// States until the engine knows which package that is.
const statesCall = "vukaStates__"

// embedsFormState reports whether st embeds ui.FormState (the ui package
// by its import in f, or FormState unqualified, as the ui package's own
// files write it).
func embedsFormState(st *ast.StructType, f *ast.File) bool {
	for _, fl := range st.Fields.List {
		if len(fl.Names) > 0 {
			continue
		}
		switch t := fl.Type.(type) {
		case *ast.Ident:
			if t.Name == "FormState" {
				return true
			}
		case *ast.SelectorExpr:
			if x, ok := t.X.(*ast.Ident); ok && t.Sel.Name == "FormState" && importsUIAs(f, x.Name) {
				return true
			}
		}
	}
	return false
}

// importsUIAs reports whether f imports the ui package as name.
func importsUIAs(f *ast.File, name string) bool {
	for _, im := range f.Imports {
		path, _ := strconv.Unquote(im.Path.Value)
		if path != DefaultJSXTarget {
			continue
		}
		if im.Name == nil && name == "ui" || im.Name != nil && im.Name.Name == name {
			return true
		}
	}
	return false
}
