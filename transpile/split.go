package transpile

import (
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"regexp"
	"strconv"
	"strings"
)

// Split is a .vuka file as templates and the rest: each function whose
// body returns markup and nothing else is a template of the .vuka.html file
// beside it, the Render of the type the file is named after its first
// template, written with bare names; Vuka is the file without them.
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
		main       bool
	}
	var out []moved
	haveMain := false
	for _, t := range f.jsx {
		if !t.returned {
			continue
		}
		fd, ok := e.templateFunc(f, t)
		if !ok || hasIsland(t) {
			continue
		}
		start := f.leadStart(f.orig(fd.Pos()))
		fn := f.orig(fd.Pos())
		lead := string(f.src[start:fn])
		if strings.Contains(lead, "@") {
			continue // decorated: the declaration stays with its decorators
		}
		header := strings.TrimRight(string(f.src[fn:f.orig(fd.Type.Results.Pos())]), " \t")
		p := &tprint{f: f, t: t}
		isMain := fd.Recv != nil && fd.Name.Name == "Render" && fd.Type.Params.NumFields() == 0 && recvTypeName(fd) == main && !haveMain
		if isMain {
			p.strip = e.bareNames(f, fd)
		}
		p.nodes(flatten([]jsxNode{t.root}), "", false, false)
		if p.fail {
			continue
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
		out = append(out, moved{start, end, b.String(), isMain})
	}
	if len(out) == 0 {
		return nil
	}
	var rest, html, mainText strings.Builder
	last := 0
	for _, m := range out {
		rest.Write(f.src[last:m.start])
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

// htmlImports are the import lines of f a template file's text needs: the
// packages it names, but the JSX target.
func (e *engine) htmlImports(f *fileState, text string) string {
	var b strings.Builder
	for _, imp := range packageImports([]File{{Name: f.name, Src: f.src}}) {
		if imp.path == f.target.path || !regexp.MustCompile(`(^|[^\w.])`+regexp.QuoteMeta(imp.name)+`\.`).MatchString(text) {
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

// hasIsland reports whether a tree places an island: those stay in Vuka
// for now.
func hasIsland(t *jsxTree) bool {
	for _, c := range t.comps {
		if c.island || c.consumed {
			return true
		}
	}
	return false
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
		if name == "_" || name == "." || regexp.MustCompile(`(^|[^\w.])`+regexp.QuoteMeta(name)+`\.`).MatchString(body) {
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
	f     *fileState
	t     *jsxTree
	b     strings.Builder
	strip string // the receiver whose selectors are written bare
	fail  bool
}

// gocode is the source of a span as Go: nothing Vuka rewrites inside it.
func (p *tprint) gocode(s span) string {
	for _, e := range p.f.fixed {
		if e.own == nil && s.start <= e.start && e.end <= s.end && e.start < s.end {
			p.fail = true
		}
	}
	for _, t := range p.f.jsx {
		if t != p.t && t.start >= s.start && t.start < s.end {
			p.fail = true
		}
	}
	text := string(p.f.src[s.start:s.end])
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
		if n.m != nil || n.sw || n.stmt {
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
