package transpile

import (
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Split is a .vuka file as templates and the rest: each function whose
// body returns markup and nothing else is a template of the .vuka.html file
// beside it, the Render of the type the file is named after its first
// template, written with bare names; markup elsewhere — assigned, passed,
// returned mid-way, or the body of a decorated function — is a partial
// there, its parameters the variables it reads, called in its place. Vuka
// is the file without them.
type Split struct {
	Vuka []byte
	HTML []byte // nil when no function moves
}

// split writes f's templates out of it.
func (e *engine) split(f *fileState) *Split {
	if f.html != nil || e.info == nil {
		return nil
	}
	main := componentName(strings.TrimSuffix(f.name, ".vuka") + ".vuka.html")
	type moved struct {
		start, end int
		text       string
		call       string // what the markup's place has now
		main       bool
	}
	var out []moved
	haveMain := false
	taken := e.takenNames(f)
	var inner []string // the partials of trees inside others
	for _, t := range f.jsx {
		if f.nested(t) {
			continue
		}
		fd, whole := e.templateFunc(f, t)
		if whole && t.returned {
			start := f.leadStart(f.orig(fd.Pos()))
			fn := f.orig(fd.Pos())
			lead := string(f.src[start:fn])
			if !strings.Contains(lead, "@") {
				header := strings.TrimRight(string(f.src[fn:f.orig(fd.Type.Results.Pos())]), " \t")
				mark := len(inner)
				p := &tprint{f: f, t: t, nest: e.nester(f, taken, &inner)}
				// A stateful component's Render is the file's main markup, read with
				// bare names; a struct component's Render keeps its header, since a
				// header-less template is a struct's View.
				isMain := fd.Recv != nil && fd.Name.Name == "Render" && fd.Type.Params.NumFields() == 0 && recvTypeName(fd) == main && !haveMain && e.embedsLive(main)
				if isMain {
					p.strip = e.bareNames(f, fd)
				}
				p.nodes(flatten([]jsxNode{t.root}), "", false, false)
				if p.fail {
					inner = inner[:mark]
				} else {
					for _, a := range p.assets {
						p.b.WriteString("\n\n" + a)
					}
					var b strings.Builder
					b.WriteString(lead)
					if !isMain || p.strip == "" {
						b.WriteString(header + "\n")
						isMain = false
					}
					b.WriteString(strings.TrimLeft(p.b.String(), "\n") + "\n")
					end := f.orig(fd.End())
					if end < len(f.src) && f.src[end] == '\n' {
						end++
					}
					haveMain = haveMain || isMain
					out = append(out, moved{start: start, end: end, text: b.String(), main: isMain})
					continue
				}
			}
		}
		if text, call, _, ok := e.partial(f, t, false, taken, &inner); ok {
			out = append(out, moved{start: t.start, end: t.end, text: text, call: call})
		}
	}
	if len(out) == 0 {
		return nil
	}
	for _, text := range inner {
		out = append(out, moved{start: len(f.src), end: len(f.src), text: text})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].start < out[j].start })
	var rest, html, mainText strings.Builder
	last := 0
	for _, m := range out {
		rest.Write(f.src[last:m.start])
		rest.WriteString(m.call)
		last = m.end
		if m.main {
			mainText.WriteString(m.text)
			continue
		}
		if html.Len() > 0 {
			html.WriteString("\n")
		}
		html.WriteString(m.text)
	}
	rest.Write(f.src[last:])
	text := mainText.String()
	vuka := dropUnusedImports(rest.String())
	if text != "" && html.Len() > 0 {
		text += "\n"
	}
	text += html.String()
	return &Split{Vuka: []byte(squeezeBlank(vuka)), HTML: []byte(e.htmlImports(f, text) + text)}
}

// nestedIn reports whether t is inside a tree in s other than outer.
func (f *fileState) nestedIn(t *jsxTree, s span, outer *jsxTree) bool {
	for _, o := range f.jsx {
		if o != t && o != outer && o.start >= s.start && o.start <= t.start && t.end <= o.end {
			return true
		}
	}
	return false
}

// nested reports whether t is inside another tree: it moves with it, or
// stays.
func (f *fileState) nested(t *jsxTree) bool {
	for _, o := range f.jsx {
		if o != t && o.start <= t.start && t.end <= o.end {
			return true
		}
	}
	return false
}

// takenNames are the names a partial can't take: the package's, and those
// of f.
func (e *engine) takenNames(f *fileState) map[string]bool {
	out := map[string]bool{}
	for _, n := range e.pkg.Scope().Names() {
		out[n] = true
	}
	ast.Inspect(f.ast, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			out[id.Name] = true
		}
		return true
	})
	return out
}

// partial is t as a partial of the template file, and its call: its
// parameters the variables of the function it reads, in the order it
// first reads them; its name the variable it is assigned to, after the
// component it is in (cardHead), or the component's (cardView).
func (e *engine) partial(f *fileState, t *jsxTree, nested bool, taken map[string]bool, inner *[]string) (text, call string, args []string, ok bool) {
	fd := f.funcAt(t.start)
	if fd != nil && (fd.Type.TypeParams != nil || fd.Recv != nil && recvGeneric(fd)) {
		return "", "", nil, false
	}
	names := map[string]string{} // import path → its name in f
	for _, imp := range packageImports([]File{{Name: f.name, Src: f.src}}) {
		names[imp.path] = imp.name
	}
	qual := func(p *types.Package) string {
		if p == e.pkg {
			return ""
		}
		if n, ok := names[p.Path()]; ok {
			return n
		}
		ok = false
		return p.Name()
	}
	ok = true
	var params []string
	seen := map[types.Object]bool{}
	ast.Inspect(f.ast, func(n ast.Node) bool {
		id, isID := n.(*ast.Ident)
		if !isID || !ok {
			return ok
		}
		off := f.orig(id.Pos())
		if off < t.start || off >= t.end || off+len(id.Name) > len(f.src) || string(f.src[off:off+len(id.Name)]) != id.Name {
			return true
		}
		obj := e.info.Uses[id]
		if obj == nil || seen[obj] || obj.Parent() == e.pkg.Scope() || obj.Parent() == types.Universe || obj.Pkg() != e.pkg {
			return true
		}
		if d := f.orig(obj.Pos()); d >= t.start && d < t.end {
			return true
		}
		switch o := obj.(type) {
		case *types.Var:
			if o.IsField() {
				return true
			}
			seen[obj] = true
			params = append(params, o.Name()+" "+types.TypeString(o.Type(), qual))
			args = append(args, o.Name())
		case *types.Const, *types.TypeName:
			ok = false // a local type or constant
		}
		return true
	})
	if !ok {
		return "", "", nil, false
	}
	name := partialName(f, t, fd, nested, taken)
	taken[name] = true
	mark := len(*inner)
	p := &tprint{f: f, t: t, nest: e.nester(f, taken, inner)}
	p.nodes(flatten([]jsxNode{t.root}), "", false, false)
	if p.fail || len(p.assets) > 0 {
		*inner = (*inner)[:mark]
		return "", "", nil, false
	}
	text = "func " + name + "(" + strings.Join(params, ", ") + ")\n" + strings.TrimLeft(p.b.String(), "\n") + "\n"
	return text, name + "(" + strings.Join(args, ", ") + ")", args, true
}

// nester makes the trees inside a tree's Go partials, kept in inner.
func (e *engine) nester(f *fileState, taken map[string]bool, inner *[]string) func(*jsxTree) (string, []string, bool) {
	return func(t *jsxTree) (string, []string, bool) {
		text, call, args, ok := e.partial(f, t, true, taken, inner)
		if ok {
			*inner = append(*inner, text)
		}
		return call, args, ok
	}
}

var assignedTo = regexp.MustCompile(`(\w+)\s*(:=|[^=!<>:+\-*/%&|^]=)\s*\(?\s*$`)

// partialName is the name of t's partial: the variable it is assigned to,
// after the declaration it is in (cardHead), its root's tag, inside the Go
// of another tree (pageEm), or the declaration's (cardView).
func partialName(f *fileState, t *jsxTree, fd *ast.FuncDecl, nested bool, taken map[string]bool) string {
	decl := f.declName(t.start, fd)
	v := ""
	if m := assignedTo.FindSubmatch(f.src[lineStart(f.src, t.start):t.start]); m != nil && string(m[1]) != decl {
		v = string(m[1])
	}
	base := lowerFirst(decl) + exportName(v)
	switch {
	case v != "":
	case nested && !t.root.frag:
		base += exportName(lastName(t.root.tag))
	default:
		base += "View"
	}
	name := base
	for i := 2; taken[name] || token.IsKeyword(name); i++ {
		name = base + strconv.Itoa(i)
	}
	return name
}

// declName is the name of the top-level declaration off is in, as the
// source writes it: a function's, a method's type, a variable's.
func (f *fileState) declName(off int, fd *ast.FuncDecl) string {
	ident := func(id *ast.Ident) string {
		at := f.orig(id.Pos())
		end := at
		for end < len(f.src) && isIdentByte(f.src[end]) {
			end++
		}
		return string(f.src[at:end])
	}
	if fd != nil {
		if fd.Recv != nil {
			if n := recvTypeName(fd); n != "" {
				return n
			}
		}
		return ident(fd.Name)
	}
	for _, d := range f.ast.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, sp := range g.Specs {
			if vs, ok := sp.(*ast.ValueSpec); ok && f.orig(vs.Pos()) <= off && off < f.orig(vs.End()) {
				return ident(vs.Names[0])
			}
		}
	}
	return "markup"
}

// funcAt is the function declaration off is in, or nil.
func (f *fileState) funcAt(off int) *ast.FuncDecl {
	for _, d := range f.ast.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil && f.orig(fd.Pos()) <= off && off < f.orig(fd.End()) {
			return fd
		}
	}
	return nil
}

func recvGeneric(fd *ast.FuncDecl) bool {
	t := fd.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	switch t.(type) {
	case *ast.IndexExpr, *ast.IndexListExpr:
		return true
	}
	return false
}

// htmlImports are the import lines of f a template file's text needs: the
// packages it names, but the JSX target.
func (e *engine) htmlImports(f *fileState, text string) string {
	var b strings.Builder
	for _, imp := range packageImports([]File{{Name: f.name, Src: f.src}}) {
		if imp.path == f.target.path || !namesImport(text, imp) {
			continue
		}
		if imp.name == pathBase(imp.path) {
			b.WriteString("import " + strconv.Quote(imp.path) + "\n")
		} else {
			b.WriteString("import " + imp.name + " " + strconv.Quote(imp.path) + "\n")
		}
	}
	if b.Len() > 0 {
		b.WriteString("\n")
	}
	return b.String()
}

// namesImport reports whether text names an import: a package by its selectors,
// a component file by its tags.
func namesImport(text string, imp goImport) bool {
	if componentFile(imp.path) {
		return regexp.MustCompile(`</?` + regexp.QuoteMeta(imp.name) + `\b`).MatchString(text)
	}
	return regexp.MustCompile(`(^|[^\w.])` + regexp.QuoteMeta(imp.name) + `\.`).MatchString(text)
}

// componentFile reports whether an import is a component file's, ./x.vue.
func componentFile(path string) bool {
	return strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../")
}

// dropUnusedImports is src without the imports its code no longer names.
func dropUnusedImports(src string) string {
	fset := token.NewFileSet()
	a, _ := parser.ParseFile(fset, "", src, parser.ImportsOnly)
	if a == nil {
		return src
	}
	body := src[fset.Position(a.End()).Offset:]
	var cut [][2]int
	for _, s := range a.Imports {
		path, _ := strconv.Unquote(s.Path.Value)
		name := pathBase(path)
		if s.Name != nil {
			name = s.Name.Name
		}
		if name == "_" || name == "." || namesImport(body, goImport{name, path}) {
			continue
		}
		start, end := fset.Position(s.Pos()).Offset, fset.Position(s.End()).Offset
		for start > 0 && (src[start-1] == ' ' || src[start-1] == '\t') {
			start--
		}
		if end < len(src) && src[end] == '\n' {
			end++
		}
		cut = append(cut, [2]int{start, end})
	}
	for i := len(cut) - 1; i >= 0; i-- {
		src = src[:cut[i][0]] + src[cut[i][1]:]
	}
	return src
}

// squeezeBlank is s with runs of blank lines a removal left made one.
func squeezeBlank(s string) string {
	return regexp.MustCompile(`\n{3,}`).ReplaceAllString(s, "\n\n")
}

// embedsLive reports whether the package's type name is a struct embedding
// a type named Live (ui.Live): a stateful component.
func (e *engine) embedsLive(name string) bool {
	if e.pkg == nil {
		return false
	}
	tn, _ := e.pkg.Scope().Lookup(name).(*types.TypeName)
	if tn == nil {
		return false
	}
	st, ok := tn.Type().Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := 0; i < st.NumFields(); i++ {
		if f := st.Field(i); f.Embedded() && f.Name() == "Live" {
			return true
		}
	}
	return false
}

func recvTypeName(fd *ast.FuncDecl) string {
	t := fd.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// templateFunc is the function a tree is the one statement of, returning
// it: func X(…) Node { return <…> }.
func (e *engine) templateFunc(f *fileState, t *jsxTree) (*ast.FuncDecl, bool) {
	ret := returnAt(f.src, t.start)
	for _, d := range f.ast.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil || len(fd.Body.List) == 0 || f.orig(fd.Body.Pos()) > ret || f.orig(fd.Body.End()) < t.end {
			continue
		}
		for _, st := range fd.Body.List {
			if off := f.orig(st.Pos()); off < ret || off > t.end {
				return nil, false // Go of its own beside the markup
			}
		}
		rs, ok := fd.Body.List[len(fd.Body.List)-1].(*ast.ReturnStmt)
		if !ok || len(rs.Results) != 1 || f.orig(rs.Pos()) != ret || fd.Type.Results == nil || fd.Type.Results.NumFields() != 1 {
			return nil, false
		}
		obj, _ := e.info.Defs[fd.Name].(*types.Func)
		node := e.nodeType(f.target)
		if obj == nil || node == nil || !types.Identical(obj.Signature().Results().At(0).Type(), node) {
			return nil, false
		}
		// The signature must read as the source writes it: Go, no Vuka.
		from, to := f.orig(fd.Pos()), f.orig(fd.Type.Results.End())
		if string(f.src[from:to]) != string(f.text[f.off(fd.Pos()):f.off(fd.Type.Results.End())]) {
			return nil, false
		}
		return fd, true
	}
	return nil, false
}

// bareNames is the receiver name a Render's template drops, writing its
// fields and methods by bare name; "" when a name would mean something
// else bare (a package-level name, a variable of the template).
func (e *engine) bareNames(f *fileState, fd *ast.FuncDecl) string {
	if len(fd.Recv.List[0].Names) != 1 {
		return ""
	}
	recv := fd.Recv.List[0].Names[0]
	obj := e.info.Defs[recv]
	ok := true
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if id, isID := n.X.(*ast.Ident); isID && e.info.Uses[id] == obj {
				if e.pkg.Scope().Lookup(n.Sel.Name) != nil || types.Universe.Lookup(n.Sel.Name) != nil {
					ok = false
				}
				return false
			}
		case *ast.Ident:
			if e.info.Uses[n] == obj {
				ok = false // the receiver itself
			}
			if d := e.info.Defs[n]; d != nil {
				if o, _, _ := types.LookupFieldOrMethod(obj.Type(), true, e.pkg, n.Name); o != nil {
					ok = false // a variable shadowing a field
				}
			}
		}
		return ok
	})
	if !ok {
		return ""
	}
	return recv.Name
}

// tprint writes a tree as a template's markup.
type tprint struct {
	f      *fileState
	t      *jsxTree
	b      strings.Builder
	strip  string                                                 // the receiver whose selectors are written bare
	nest   func(t *jsxTree) (call string, args []string, ok bool) // a tree inside the Go of this one, as a partial
	fail   bool
	assets []string // the root's scripts and scoped stylesheets, written after the markup
}

// gocode is the source of a span as Go: nothing Vuka rewrites inside it.
func (p *tprint) gocode(s span) string {
	for _, e := range p.f.fixed {
		if e.own == nil && s.start <= e.start && e.end <= s.end && e.start < s.end {
			p.fail = true
		}
	}
	var b0 strings.Builder
	last := s.start
	for _, t := range p.f.jsx {
		if t == p.t || t.start < s.start || t.start >= s.end || p.f.nestedIn(t, s, p.t) {
			continue
		}
		call, args, ok := p.nest(t)
		if !ok || t.end > s.end || slices.Contains(args, p.strip) {
			p.fail = true
			continue
		}
		b0.Write(p.f.src[last:t.start])
		b0.WriteString(call)
		last = t.end
	}
	b0.Write(p.f.src[last:s.end])
	text := b0.String()
	if p.strip == "" {
		return text
	}
	var sc scanner.Scanner
	fs := token.NewFileSet()
	sc.Init(fs.AddFile("", -1, len(text)), []byte(text), nil, scanner.ScanComments)
	var b strings.Builder
	last, prev := 0, token.ILLEGAL
	for {
		pos, tok, lit := sc.Scan()
		if tok == token.EOF {
			break
		}
		off := fs.Position(pos).Offset
		if tok == token.IDENT && lit == p.strip && prev != token.PERIOD {
			// recv.X is X: drop "recv."
			b.WriteString(text[last:off])
			last = off + len(lit)
			if _, t, _ := sc.Scan(); t == token.PERIOD {
				last++
			}
			prev = token.PERIOD
			continue
		}
		prev = tok
	}
	b.WriteString(text[last:])
	return b.String()
}

var stmtLike = regexp.MustCompile(`^(if|for|switch)\b|^[A-Za-z_][\w.]*\s*(:=|=[^=])`)

// nodes writes ns at indent, each on a line of its own where whitespace
// between them means nothing — always in a block's body — else inline.
func (p *tprint) nodes(ns []jsxNode, indent string, inline, body bool) {
	for _, n := range ns {
		if el, ok := n.(*jsxElem); ok && el.asset {
			p.assets = append(p.assets, string(p.f.src[el.start:el.end]))
		}
	}
	ns = withoutAssets(ns)
	blocks := false
	for _, n := range ns {
		if _, ok := n.(*jsxBlock); ok {
			blocks = true
		}
	}
	breaks := !inline && (layout(ns) || blocks || body && len(ns) > 0)
	for i, n := range ns {
		lineStart := breaks && (i == 0 || isMarkup(ns[i-1]) && isMarkup(n))
		if _, ok := n.(*jsxBlock); ok && !lineStart {
			p.fail = true // a statement starts a line
		}
		if lineStart {
			p.b.WriteString("\n" + indent)
		}
		p.node(n, indent, inline, lineStart || i == 0)
	}
	if breaks && indent != "" {
		p.b.WriteString("\n" + indent[2:])
	}
	if body && !breaks {
		p.b.WriteString("\n" + indent[2:])
	}
}

func (p *tprint) node(n jsxNode, indent string, inline, lineStart bool) {
	switch n := n.(type) {
	case *jsxText:
		v := tmplTextEscaper.Replace(n.val)
		if lineStart && stmtLike.MatchString(v) {
			word := regexp.MustCompile(`^[\w.]+`).FindString(v)
			v = "{" + `"` + word + `"` + "}" + v[len(word):]
		}
		p.b.WriteString(v)
	case *jsxHole:
		p.b.WriteString("{" + p.gocode(n.expr) + "}")
	case *jsxBlock:
		if n.m != nil {
			p.match(n, indent)
			return
		}
		if n.sw || n.stmt {
			p.fail = true
			return
		}
		for i, h := range n.heads {
			if i > 0 {
				p.b.WriteString("} ")
			}
			p.b.WriteString(p.gocode(h))
			p.nodes(flatten(n.bodies[i]), indent+"  ", false, true)
		}
		p.b.WriteString("}")
	case *jsxElem:
		if n.frag {
			p.nodes(flatten(n.kids), indent, inline, false)
			return
		}
		p.element(n, indent, inline)
	}
}

// match writes a {match} whose patterns are values as a switch; one that
// binds or guards stays in the .vuka.
func (p *tprint) match(n *jsxBlock, indent string) {
	m := n.m
	p.b.WriteString("switch " + p.gocode(m.subj) + " {")
	for i, c := range m.cases {
		var pats []string
		for _, pt := range c.pats {
			text := strings.TrimSpace(string(p.f.src[pt.start:pt.end]))
			x, err := parser.ParseExpr(text)
			if _, lit := x.(*ast.BasicLit); text == "_" && len(c.pats) == 1 {
				continue
			} else if err != nil || !lit {
				p.fail = true
				return
			}
			pats = append(pats, text)
		}
		if c.guard.start >= 0 {
			p.fail = true
			return
		}
		if i == 0 {
			p.b.WriteString("\n" + indent)
		}
		if len(pats) == 0 {
			p.b.WriteString("default:")
		} else {
			p.b.WriteString("case " + strings.Join(pats, ", ") + ":")
		}
		p.nodes(flatten(n.cases[i]), indent+"  ", false, true)
	}
	if len(m.cases) == 0 {
		p.b.WriteString("\n" + indent)
	}
	p.b.WriteString("}")
}

func (p *tprint) element(el *jsxElem, indent string, inline bool) {
	tag := string(p.f.src[el.start+1 : el.tagEnd])
	p.b.WriteString("<" + tag)
	for _, a := range el.attrs {
		name := a.name
		if el.comp == nil {
			name = htmlName(name)
		}
		switch a.kind {
		case 's':
			p.b.WriteString(" " + name + `="` + tmplAttrEscaper.Replace(a.val) + `"`)
		case 'b':
			p.b.WriteString(" " + name)
		case 'e':
			p.b.WriteString(" " + name + "={" + p.gocode(a.expr) + "}")
		default:
			p.fail = true
		}
	}
	kids := flatten(el.kids)
	if len(kids) == 0 {
		p.b.WriteString(" />")
		return
	}
	p.b.WriteString(">")
	switch el.tag {
	case "script", "style":
		p.fail = true
	case "pre", "textarea", "title":
		p.nodes(kids, indent+"  ", true, false)
	default:
		p.nodes(kids, indent+"  ", inline, false)
	}
	p.b.WriteString("</" + tag + ">")
}

var (
	tmplTextEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", "{", "{{", "}", `{"}"}`)
	tmplAttrEscaper = strings.NewReplacer("&", "&amp;", `"`, "&quot;", "{", "{{")
)
