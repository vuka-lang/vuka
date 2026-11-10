package transpile

import (
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// A .vuka.html file is Go above markup: its package clause and imports, then
// a header of Go — the struct its markup renders, the decorated hooks and
// methods of it, other helpers — up to the first line starting with <, then
// the markup that struct renders. A file declaring several structs renders
// the one named after it; the others are helpers, as in a .go file:
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
	Helpers  []string // the file's other structs: they are data, not components

	// Live is whether the struct is a live component: it embeds ui.Live,
	// declares a hook, or has an exported method the markup wires to an
	// on… attribute other than as a Push or Options step's handler, which
	// a page that isn't live calls over HTTP. LiveWhy says which, for
	// messages.
	Live       bool
	LiveWhy    string
	EmbedsLive bool

	// File is the script file declaring the struct, beside a template of
	// markup alone; "" when the template's own script does. TypeLine and
	// the fields' lines are its.
	File string

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
	// BrowserState is a ui.State[T] or ui.Derived[T] field: state the
	// browser keeps.
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
	shapeType  = regexp.MustCompile(`^type\s+\w+(?:\[[^\]]*\])?\s+struct\b|^type\s*\(`)
	hookName   = regexp.MustCompile(`^@\s*\w+\s*\.\s*(Mount|OnChange|Loads)\b`)
	eventAttr  = regexp.MustCompile(`\bon[A-Z]\w*(?:\.\w+)*=\{`)
)

// isShape reports whether the template file's lines, its head ending at top,
// have a Go header: the first line after the imports that isn't blank or a
// comment starts a declaration (type, var, const) or a decorator, or a struct
// type is declared before the markup, read as goHeader reads it: a line of Go
// starting with <, a receive or a raw string's, doesn't start it. A file
// without one starts with markup, a statement or a func header: E0152.
func isShape(lines []string, top int) bool {
	for i := top; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "//") {
			continue
		}
		if shapeStart.MatchString(lines[i]) {
			return true
		}
		break
	}
	text := strings.Join(lines[min(top, len(lines)):], "")
	_, _, markup := goHeader([]byte(text), 0)
	if markup >= 0 {
		text = text[:markup]
	}
	for _, l := range strings.SplitAfter(text, "\n") {
		if shapeType.MatchString(l) {
			return true
		}
	}
	return false
}

// ParseShape is ParseShapeFile for a file whose name isn't known: a file
// declaring several structs doesn't say which it renders, E0147.
func ParseShape(src []byte) (*Shape, *Error) { return ParseShapeFile("", src) }

// ParseShapeFile reads src, the template file name, as its Go header and
// markup: nil without an error when it has no Go header (E0152 when
// compiled), and an error when the header breaks one of its rules.
func ParseShapeFile(name string, src []byte) (*Shape, *Error) {
	src, _, err := headerView(src)
	if err != nil {
		return nil, err
	}
	head := parseTemplateHead(src)
	lines := strings.SplitAfter(string(src), "\n")
	if !isShape(lines, head.Top) {
		return nil, nil
	}
	return parseShape(name, src, lines, head)
}

func parseShape(name string, src []byte, lines []string, head TemplateHead) (*Shape, *Error) {
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
	// AllErrors: what .vuka allows and the skeleton keeps isn't Go, and any
	// amount of it mustn't stop the parser short of a struct.
	f, err := parser.ParseFile(fset, "", text, parser.ParseComments|parser.AllErrors)
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
	if len(structs) == 0 {
		if err != nil {
			return nil, shapeSyntax(err)
		}
		return nil, &Error{Code: "E0146", Pos: token.Position{Line: head.Top + 1, Column: 1},
			Msg:  "the template file's Go declares no struct: a .vuka.html file renders one struct, declared above its markup",
			Help: "declare it: type Page struct { … }"}
	}
	ts, helpers, rerr := renderedStruct(name, structs, f)
	if rerr != nil {
		rerr.Pos = pos(rerr.at)
		rerr.Len = rerr.length
		return nil, rerr.Error
	}
	s.Helpers = helpers
	// A syntax error past the struct is the compiler's to report, at its
	// place, as for any .vuka source.
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
	hook := ownHook(hooks, f, fset, helpers)
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
	case hook != "":
		s.Live, s.LiveWhy = true, "it declares the hook "+hook
	default:
		if m := wiredEvent(maskFormSubmits(string(src[markup:]), uiName(text)), events); m != "" {
			s.Live, s.LiveWhy = true, "the markup sends the event "+m
		}
	}
	if s.Live {
		s.Ptr = true
	}
	return s, nil
}

// ownHook is the first hook decorating anything but a helper's method: a
// helper is data, and its hooks don't make the file's struct live.
func ownHook(hooks []headerHook, f *ast.File, fset *token.FileSet, helpers []string) string {
next:
	for _, h := range hooks {
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fset.Position(fd.Pos()).Offset < h.off {
				continue
			}
			if fd.Recv != nil && len(fd.Recv.List) == 1 {
				if _, base := shapeRecv(fd.Recv.List[0].Type); slices.Contains(helpers, base) {
					continue next
				}
			}
			break
		}
		return h.name
	}
	return ""
}

// shapeError is an error at a struct's name.
type shapeError struct {
	*Error
	at     token.Pos
	length int
}

// renderedStruct is which of a template file's structs its markup renders,
// and the others, the file's helpers: the only struct; else, a form set
// aside (a struct embedding ui.FormState or a ui.FormFor is data) and type aliases too, the
// only one left; else the one named after the file. name is the file's name,
// "" when it isn't known.
func renderedStruct(name string, structs []*ast.TypeSpec, f *ast.File) (*ast.TypeSpec, []string, *shapeError) {
	if len(structs) == 1 {
		return structs[0], nil, nil
	}
	var rendered []*ast.TypeSpec
	for _, ts := range structs {
		if ts.Assign == token.NoPos && !embedsFormState(ts.Type.(*ast.StructType), f) {
			rendered = append(rendered, ts)
		}
	}
	var pick *ast.TypeSpec
	switch {
	case len(rendered) == 1:
		pick = rendered[0]
	case len(rendered) == 0:
		rendered = structs
		fallthrough
	default:
		var named []*ast.TypeSpec
		for _, ts := range rendered {
			if name != "" && shapeNameKey(ts.Name.Name) == shapeNameKey(fileStem(name)) {
				named = append(named, ts)
			}
		}
		if len(named) != 1 {
			return nil, nil, twoStructs(name, rendered, named)
		}
		pick = named[0]
	}
	var helpers []string
	for _, ts := range structs {
		if ts != pick {
			helpers = append(helpers, ts.Name.Name)
		}
	}
	return pick, helpers, nil
}

// twoStructs is E0147, at the struct the file doesn't explain.
func twoStructs(name string, structs, named []*ast.TypeSpec) *shapeError {
	second := structs[1]
	names := make([]string, len(structs))
	for i, ts := range structs {
		names[i] = ts.Name.Name
	}
	list := strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	e := &Error{Code: "E0147"}
	switch {
	case name == "":
		e.Msg = "the template file declares " + list + ": a .vuka.html file renders one struct"
		e.Help = "move " + second.Name.Name + " to its own .vuka.html file, or to a .go file beside this one when it has no markup (a form, a struct embedding ui.FormState or a ui.FormFor, may stay)"
	case len(named) > 1:
		second = named[1]
		e.Msg = "the template file declares " + named[0].Name.Name + " and " + second.Name.Name + ", and both are named after " + fileBase(name) + ": it renders the one struct named after it"
		e.Help = "rename " + second.Name.Name
	default:
		want := ShapeTypeName(name)
		e.Msg = "the template file declares " + list + ", and none is named after it: " + fileBase(name) + " renders the struct " + want
		e.Help = "name the struct its markup renders " + want + "; the others stay as helpers. A struct with markup of its own goes in its own .vuka.html file"
		if !token.IsIdentifier(want) {
			e.Msg = "the template file declares " + list + ", and no struct can be named after " + fileBase(name)
			e.Help = "keep one struct in this file, moving the others to a .go file, or give the file a name a struct can take"
		}
	}
	return &shapeError{Error: e, at: second.Name.Pos(), length: len(second.Name.Name)}
}

// ShapeTypeName is the name of the struct a template file renders when it
// declares several: orders.vuka.html renders Orders, order-row.vuka.html and
// order_row.vuka.html OrderRow, _layout.vuka.html Layout, [id].vuka.html Id
// and [path...].vuka.html Path. Case and separators don't count when it is
// compared with the file's structs, so ID and orderRow match too.
func ShapeTypeName(file string) string { return componentName(fileBase(file)) }

// fileBase is the file's name without its directory.
func fileBase(file string) string {
	return file[strings.LastIndexAny(file, `/\`)+1:]
}

// fileStem is the file's base name without .vuka.html.
func fileStem(file string) string {
	return strings.TrimSuffix(fileBase(file), ".vuka.html")
}

// shapeNameKey is a name with case and separators gone, as a file's name and
// a struct's are compared.
func shapeNameKey(name string) string {
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// implicitLive is E0169 for a shape needing a live connection without
// embedding ui.Live. h.shape is the struct the file renders, never a helper.
func (e *engine) implicitLive(f *fileState) {
	h := f.html
	if h == nil || h.shape == nil || !h.shape.Live || h.shape.EmbedsLive {
		return
	}
	file := f.path
	for _, g := range e.files {
		if h.shape.File != "" && g.name == h.shape.File {
			file = g.path
		}
	}
	e.errs.add("E0169", token.Position{Filename: file, Line: h.shape.TypeLine, Column: 1}, "%s needs a live connection because %s", h.shape.Type, h.shape.LiveWhy).
		help("embed ui.Live in %s (vuka fix explicit-live)", h.shape.Type)
}

// EmbedLive is EmbedLiveFile for a file whose name isn't known: one declaring
// several structs doesn't say which it renders, so nothing is done.
func EmbedLive(src []byte) (out []byte, why string) { return EmbedLiveFile("", src) }

// EmbedLiveFile adds ui.Live to the struct the .vuka.html file renders,
// when that struct is live by anything else, with the import it
// needs; its helpers stay as they are. It returns the new source and why the
// struct was live; nil when there is nothing to do.
func EmbedLiveFile(file string, src []byte) (out []byte, why string) {
	s, err := ParseShapeFile(file, src)
	if err != nil || s == nil || !s.Live || s.EmbedsLive {
		return nil, ""
	}
	name := uiName(string(src))
	head, tail := string(src[:s.open]), string(src[s.open:])
	if !uiImport.MatchString(string(src)) {
		head = addUIImport(head)
	}
	if strings.HasSuffix(head, "struct{") {
		head = strings.TrimSuffix(head, "{") + " {"
	}
	field := "\n\t" + name + ".Live"
	if tail != "" && tail[0] != '\n' {
		// All on one line: struct{} or struct{ n int }.
		line, rest, _ := strings.Cut(tail, "\n")
		fields := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), "}"))
		if fields != "" {
			field += "\n\t" + fields
		}
		tail = "\n}\n" + rest
	}
	return []byte(head + field + tail), s.LiveWhy
}

// addUIImport adds ui to the imports of head, a file's text up to its struct:
// to its import block, or to its one import, making that a block.
func addUIImport(head string) string {
	spec := "\t\"" + uiImportPath + "\"\n"
	if at := importBlock.FindStringIndex(head); at != nil {
		if end := strings.Index(head[at[1]:], ")"); end >= 0 {
			return head[:at[1]+end] + spec + head[at[1]+end:]
		}
	}
	if at := loneImport.FindStringSubmatchIndex(head); at != nil {
		return head[:at[0]] + "import (\n\t" + head[at[2]:at[3]] + "\n" + spec + ")" + head[at[1]:]
	}
	if at := uiPackageClause.FindStringIndex(head); at != nil {
		return head[:at[1]] + "\n\nimport \"" + uiImportPath + "\"" + head[at[1]:]
	}
	return head
}

// uiName is what a file calls the ui package.
func uiName(text string) string {
	if m := uiImport.FindStringSubmatch(text); m != nil && m[1] != "" {
		return strings.TrimSpace(m[1])
	}
	return "ui"
}

const uiImportPath = "github.com/vuka-lang/ui/v2"

var (
	uiImport        = regexp.MustCompile(`(?m)^\s*(?:import\s+)?(\w+\s+)?"` + regexp.QuoteMeta(uiImportPath) + `"`)
	uiPackageClause = regexp.MustCompile(`(?m)^package\s+\w+[ \t]*$`)
	importBlock     = regexp.MustCompile(`(?m)^import\s*\(`)
	loneImport      = regexp.MustCompile(`(?m)^import\s+((?:\w+\s+)?"[^"]*")[ \t]*$`)
)

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

// isUIState reports whether x is State[T] or Derived[T] of a package,
// ui.State[string].
func isUIState(x ast.Expr) bool {
	ix, ok := x.(*ast.IndexExpr)
	if !ok {
		return false
	}
	switch t := ix.X.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name == "State" || t.Sel.Name == "Derived"
	case *ast.Ident:
		return t.Name == "State" || t.Name == "Derived"
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

// PushesMethod reports whether a Push or Options step in src's markup names
// the method name: p.Name, or a bare Name.
func PushesMethod(src []byte, name string) bool {
	_, _, markup := goHeader(HeaderView(src), 0)
	if markup < 0 {
		return false
	}
	m := string(src[markup:])
	step := regexp.MustCompile(`\.(?:Push|Options)\(\s*(?:\w+\.)?` + regexp.QuoteMeta(name) + `\b`)
	for _, loc := range eventAttr.FindAllStringIndex(m, -1) {
		if end := holeEnd(m, loc[1]); end >= 0 && step.MatchString(m[loc[1]:end]) {
			return true
		}
	}
	return false
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
		hole := markup[loc[1]:end]
		if methodCall(hole) {
			continue // a push, reaching the method over HTTP on a page that isn't live
		}
		if m := name.FindStringSubmatch(maskPushSteps(hole)); m != nil {
			return m[1]
		}
	}
	return ""
}

var callHead = regexp.MustCompile(`^\s*(?:\w+\.)?[A-Z]\w*\s*\(`)

// methodCall reports whether an on… attribute's expression is one call of
// an exported name, Choose(this.value): a push of the method it names.
func methodCall(hole string) bool {
	m := callHead.FindStringIndex(hole)
	if m == nil {
		return false
	}
	args := closeParen(hole, m[1])
	return m[1]+len(args) < len(hole) && strings.TrimSpace(hole[m[1]+len(args)+1:]) == ""
}

var (
	formSubmit = regexp.MustCompile(`\bonSubmit=\{\s*(?:\w+\.)?\w+\s*\}`)
	formFor    = regexp.MustCompile(`\bfor=\{`)
)

// maskFormSubmits is markup without the onSubmit naming a method on each
// <ui.Form for={…}>: a struct form posts over HTTP on a page that isn't live,
// whatever its handler's name, so its handler doesn't make the page live.
func maskFormSubmits(markup, ui string) string {
	open := "<" + ui + ".Form"
	for from := 0; ; {
		at := strings.Index(markup[from:], open)
		if at < 0 {
			return markup
		}
		start := from + at
		from = start + len(open)
		if from < len(markup) && (markup[from] == '_' || markup[from] >= '0' && markup[from] <= '9' || markup[from] >= 'a' && markup[from] <= 'z' || markup[from] >= 'A' && markup[from] <= 'Z') {
			continue // <ui.FormField
		}
		end := openTagEnd(markup, from)
		tag := markup[start:end]
		if !formFor.MatchString(tag) {
			continue
		}
		markup = markup[:start] + formSubmit.ReplaceAllStringFunc(tag, func(m string) string {
			return strings.Repeat(" ", len(m))
		}) + markup[end:]
	}
}

// openTagEnd is where the open tag whose attributes start at i ends, holes and
// quoted values skipped; len(s) when it doesn't.
func openTagEnd(s string, i int) int {
	for ; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\'':
			for i++; i < len(s) && s[i] != c; i++ {
			}
		case '{':
			end := holeEnd(s, i+1)
			if end < 0 {
				return len(s)
			}
			i = end
		case '>':
			return i + 1
		}
	}
	return len(s)
}

var pushStep = regexp.MustCompile(`\.(?:Push|Options|Before|Success|Error|After|OnSuccess|OnError)\(`)

// maskPushSteps is an on… attribute's expression without the arguments of
// its Push and Options steps and of their hooks: a method a step names
// doesn't make its page live, the step reaches it over HTTP on a page that
// isn't. A closure among them does, running only on a live instance.
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
// kept, with what is .vuka and not Go written as spaces (attributes and
// decorators, function bodies with their ?s and matches, enums, decorator
// declarations, static fields, the type of func T.New) —, the hooks its decorators name, and
// the offset its markup starts at, -1 when nothing starts it.
func goHeader(src []byte, from int) (skel string, hooks []headerHook, markup int) {
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
	lineEnd := map[int]bool{} // the tokens a line's automatic semicolon follows
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
			if len(toks) > 0 {
				lineEnd[len(toks)-1] = true
			}
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
	// declEnd is the index of the token closing the first {…} from toks[i],
	// the parens and brackets before it skipped.
	declEnd := func(i int) int {
		for j := i; j < len(toks); j++ {
			switch toks[j].t {
			case token.LPAREN, token.LBRACK:
				j = match(j)
			case token.LBRACE:
				return match(j)
			}
		}
		return len(toks) - 1
	}
	// fieldEnd is the index of the last token of the field starting at toks[i].
	fieldEnd := func(i int) int {
		for j := i; j < len(toks); j++ {
			switch toks[j].t {
			case token.LBRACE, token.LPAREN, token.LBRACK:
				j = match(j)
			case token.SEMICOLON:
				return j - 1
			case token.RBRACE:
				return j - 1
			}
			if lineEnd[j] {
				return j
			}
		}
		return len(toks) - 1
	}
	depth = 0
	inFunc := false
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		next := func(n int) token.Token {
			if i+n < len(toks) {
				return toks[i+n].t
			}
			return token.EOF
		}
		switch {
		case t.t == token.IDENT && depth == 0 && lineStart(t.off) && next(1) == token.IDENT && (t.lit == "enum" || t.lit == "decorator"):
			// An enum, a decorator: enum Kind {…}, decorator d(c) {…},
			// decorator D = @a @b.
			last := declEnd(i)
			if next(2) == token.ASSIGN {
				for last = i + 2; last < len(toks)-1 && !lineEnd[last]; last++ {
				}
			}
			blankRange(t.off, end(last))
			i = last
		case t.t == token.IDENT && depth > 0 && t.lit == "static" && (lineStart(t.off) || toks[i-1].t == token.SEMICOLON || toks[i-1].t == token.LBRACE) && (next(1) == token.IDENT || next(1) == token.CONST):
			// A static field, its value with it: it is no field of the struct.
			last := fieldEnd(i)
			blankRange(t.off, end(last))
			i = last
		case t.t == token.FUNC && depth == 0 && next(1) == token.IDENT && (next(2) == token.PERIOD || next(2) == token.LBRACK && i+2 < len(toks) && match(i+2)+1 < len(toks) && toks[match(i+2)+1].t == token.PERIOD):
			// func T.New(…) is a function New to the skeleton.
			dot := i + 2
			if toks[dot].t == token.LBRACK {
				dot = match(dot) + 1
			}
			blankRange(toks[i+1].off, end(dot))
			inFunc = true
			i = dot
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
					hooks = append(hooks, headerHook{name.String(), from + t.off})
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

// headerHook is a hook's decorator in a header: its name and where it is.
type headerHook struct {
	name string
	off  int
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

// embedsFormState reports whether st embeds ui.FormState or a
// ui.FormFor[T] — a form, data — (the ui package by its import in f, or
// the names unqualified, as the ui package's own files write them).
func embedsFormState(st *ast.StructType, f *ast.File) bool {
	for _, fl := range st.Fields.List {
		if len(fl.Names) > 0 {
			continue
		}
		t, generic := fl.Type, false
		if ix, ok := t.(*ast.IndexExpr); ok {
			t, generic = ix.X, true
		}
		name := "FormState"
		if generic {
			name = "FormFor"
		}
		switch t := t.(type) {
		case *ast.Ident:
			if t.Name == name {
				return true
			}
		case *ast.SelectorExpr:
			if x, ok := t.X.(*ast.Ident); ok && t.Sel.Name == name && importsUIAs(f, x.Name) {
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
