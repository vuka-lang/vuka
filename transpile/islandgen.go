package transpile

import (
	"bytes"
	"fmt"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/vuka-lang/vuka/internal/tsprops"
)

// Imported islands. A .vuka file may import a client component file:
//
//	import Counter "./islands/Counter.vue"
//
// The compiler reads the component's props from the file (tsprops: Vue's
// defineProps/defineModel/defineEmits, a React component's props, Svelte's
// $props(), a vanilla mount's) and generates the island's Go props type in
// the file's trailer — a struct named Counter, the types its props need,
// and its registration — so <Counter count={5}/> is type-checked like any
// component's props. An @ui.Island struct of the same name, declared in the
// package, wins: the import then only names it.
//
// The TypeScript → Go mapping (docs/web/islands.md):
//
//	string → string; number → float64; boolean → bool; Date → time.Time
//	'a' | 'b' → a named string type, its values as constants (other literals are refused)
//	T[], Array<T> → []T; Record<string, T>, {[k: string]: T} → map[string]T
//	an object type or interface → a struct (json tags = the prop names)
//	any, unknown → any
//	(a: A) => void, a Vue emit → func(a A): takes a live view method or JS commands
//	children, a default slot → Children ui.Node
//	optional (?, a default) or nullable (| null, | undefined) → a pointer with
//	omitempty (slices, maps, any and funcs are nil already): left out, the
//	component's default applies; a value given on the tag is wrapped for it
type islandImport struct {
	name  string // the island's Go type name
	path  string // as written
	off   int    // the import string's offset
	start int    // the import spec's (or single import declaration's) span
	end   int
	gen   bool // the compiler generates its type (no @ui.Island struct of the name)
	comp  *tsprops.Component
	enums map[string][]string // generated named string types → their values
}

// declaredIslands is the names of the package's @ui.Island types, from the
// sources.
func declaredIslands(files []File) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		if strings.HasSuffix(f.Name, ".go") {
			for _, m := range registerIsland.FindAllSubmatch(f.Src, -1) {
				out[string(m[1])] = true
			}
			continue
		}
		for _, d := range islandDecls(f.Src) {
			out[d.name] = true
		}
	}
	return out
}

// registerIsland is a Go file's registration of an island type: what
// @ui.Island lowers to, written by hand.
var registerIsland = regexp.MustCompile(`\bRegisterIsland\[(\w+)\]\(`)

// islandImports finds a file's imports of component files, blanks them, and
// generates each one's props type into the file's trailer.
func (e *engine) islandImports(f *fileState, errs *ErrorList) {
	toks := scanTokens(f.src)
	if f.pkgEnd == 0 {
		for k := 0; k+1 < len(toks); k++ {
			if toks[k].tok == token.PACKAGE {
				f.pkgEnd = toks[k+1].end()
				break
			}
		}
	}
	for i := 0; i < len(toks); i++ {
		switch toks[i].tok {
		case token.FUNC, token.TYPE, token.VAR, token.CONST:
			return
		case token.IMPORT:
		default:
			continue
		}
		kw := i // the import keyword
		spec := func(j int, single bool) int {
			nameTok := -1
			if j < len(toks) && (toks[j].tok == token.IDENT || toks[j].tok == token.PERIOD) {
				nameTok = j
				j++
			}
			if j >= len(toks) || toks[j].tok != token.STRING {
				return j
			}
			p, err := strconv.Unquote(toks[j].lit)
			if err != nil || !tsprops.IsComponent(p) {
				return j
			}
			start := toks[j].off
			if nameTok >= 0 {
				start = toks[nameTok].off
			}
			if single {
				start = toks[kw].off
			}
			end := toks[j].end()
			f.add(start, end, "")
			if nameTok < 0 || toks[nameTok].tok != token.IDENT || toks[nameTok].lit == "_" {
				errs.add("E0301", f.at(toks[j].off), "an island import names its component: import Counter %s", toks[j].lit)
				return j
			}
			e.islandImport(f, toks, &islandImport{name: toks[nameTok].lit, path: p, off: toks[j].off, start: start, end: end}, errs)
			return j
		}
		i++
		if i < len(toks) && toks[i].tok == token.LPAREN {
			for i++; i < len(toks) && toks[i].tok != token.RPAREN; i++ {
				if toks[i].tok != token.SEMICOLON && toks[i].tok != token.COMMENT {
					i = spec(i, false)
				}
			}
		} else {
			i = spec(i, true)
		}
	}
}

// islandImport reads one imported component and generates its type.
func (e *engine) islandImport(f *fileState, toks []tok, im *islandImport, errs *ErrorList) {
	if e.islands == nil {
		e.islands = map[string]*islandImport{}
	}
	if prev := e.islands[im.name]; prev != nil {
		if prev.path != im.path {
			errs.add("E0302", f.at(im.off), "island %s is imported from %s and from %s", im.name, prev.path, im.path)
		}
		return
	}
	e.islands[im.name] = im
	im.gen = !e.declaredIslands[im.name]
	if !im.gen {
		return // the @ui.Island struct is the contract
	}
	if e.dir == "" {
		errs.add("E0303", f.at(im.off), "can't read %s: the package's directory is unknown", im.path)
		return
	}
	file := filepath.Join(e.dir, filepath.FromSlash(im.path))
	src, err := readFileFn(file)
	if err != nil {
		errs.add("E0303", f.at(im.off), "can't read island component %s: %v", im.path, err)
		return
	}
	comp, err := tsprops.Read(filepath.ToSlash(file), src, func(p string) ([]byte, error) { return readFileFn(filepath.FromSlash(p)) })
	if err != nil {
		errs.add("E0304", f.at(im.off), "%s:%s; or declare the island's props with an @ui.Island struct", im.path, strings.TrimPrefix(":"+err.Error(), ":"))
		return
	}
	im.comp = comp
	tg := f.jsxTarget(toks, im.off, errs)
	if tg == nil || tg.q == "__jsx." {
		return
	}
	if !tg.has("RegisterIsland") {
		errs.add("E0305", f.at(im.off), "%s has no RegisterIsland: update it to import island components", tg.path)
		return
	}
	g := &islandGen{island: im.name, q: tg.q, names: map[string]bool{im.name: true}, enums: map[string][]string{}, file: im.path}
	text, msgs := g.decls(comp)
	for _, m := range msgs {
		errs.add("E0304", f.at(im.off), "%s; declare the island's props with an @ui.Island struct", m)
	}
	if len(msgs) > 0 {
		return
	}
	im.enums = g.enums
	if g.time {
		text = strings.ReplaceAll(text, "\x00time.", f.importAs("time", "__vktime")+".")
	}
	var w genWriter
	w.gen("\n", im.off)
	if !f.bare {
		p := f.at(im.start)
		w.gen(lineDirective(p), im.off)
	}
	w.gen(text, im.off)
	w.gen("\nfunc init() { "+tg.q+"RegisterIsland["+im.name+"]("+strconv.Quote(im.path)+") }\n", im.off)
	f.islandGen.append(&w)
}

// readFileFn reads a component file; tests may replace it.
var readFileFn = readFile

// islandGen writes the Go of an imported island's props.
type islandGen struct {
	island string
	q      string
	file   string
	names  map[string]bool     // Go type names taken
	byTS   map[string]string   // TS named types → their Go names
	enums  map[string][]string // named string types → values
	out    strings.Builder
	errs   []string
	time   bool
}

// decls is the Go of the island's props: its struct, then the types it uses.
func (g *islandGen) decls(c *tsprops.Component) (string, []string) {
	g.byTS = map[string]string{}
	var b strings.Builder
	b.WriteString("type " + g.island + " struct {\n")
	seen := map[string]string{}
	for _, p := range c.Props {
		gn := goField(p.Name)
		if gn == "" {
			g.errf(p.Line, "prop %s has no Go name", p.Name)
			continue
		}
		if other, dup := seen[gn]; dup {
			g.errf(p.Line, "props %s and %s are both %s in Go", other, p.Name, gn)
			continue
		}
		seen[gn] = p.Name
		typ, ok := g.goType(p.Type, g.island+gn, p.Line, p.Name, true)
		if !ok {
			continue
		}
		optional := p.Optional || nullable(p.Type)
		if optional {
			typ = pointerFor(typ)
		}
		tag := p.Name
		if optional {
			tag += ",omitempty"
		}
		fmt.Fprintf(&b, "\t%s %s `json:%s`\n", gn, typ, strconv.Quote(tag))
	}
	if c.Children {
		if _, dup := seen["Children"]; !dup {
			b.WriteString("\tChildren " + g.q + "Node\n")
		}
	}
	b.WriteString("}\n")
	b.WriteString(g.out.String())
	return b.String(), g.errs
}

func (g *islandGen) errf(line int, format string, args ...any) {
	g.errs = append(g.errs, fmt.Sprintf("%s:%d: %s", g.file, line, fmt.Sprintf(format, args...)))
}

// nullable reports whether a union admits null or undefined.
func nullable(t *tsprops.Type) bool {
	if t.Kind != tsprops.Union {
		return false
	}
	for _, u := range t.Union {
		if u.Kind == tsprops.Null || u.Kind == tsprops.Undefined {
			return true
		}
	}
	return false
}

// pointerFor is the type an optional prop takes: a pointer, unless the
// type is nil already.
func pointerFor(t string) string {
	if strings.HasPrefix(t, "[]") || strings.HasPrefix(t, "map[") || strings.HasPrefix(t, "func(") || t == "any" || strings.HasPrefix(t, "*") {
		return t
	}
	return "*" + t
}

// goType maps a TypeScript type to Go. hint names an anonymous object or
// union's type; top is a prop's own type (where a function may be).
func (g *islandGen) goType(t *tsprops.Type, hint string, line int, prop string, top bool) (string, bool) {
	unsupported := func(what string) (string, bool) {
		g.errf(line, "prop %s: %s has no Go type", prop, what)
		return "", false
	}
	switch t.Kind {
	case tsprops.String:
		return "string", true
	case tsprops.Number:
		return "float64", true
	case tsprops.Boolean, tsprops.BoolLit:
		return "bool", true
	case tsprops.Date:
		g.time = true
		return "\x00time.Time", true
	case tsprops.Any:
		return "any", true
	case tsprops.NumLit:
		return "float64", true
	case tsprops.StrLit:
		return g.enum(&tsprops.Type{Kind: tsprops.Union, Union: []*tsprops.Type{t}}, hint)
	case tsprops.Array:
		el, ok := g.goType(t.Elem, hint, line, prop, false)
		if !ok {
			return "", false
		}
		return "[]" + el, true
	case tsprops.Record:
		el, ok := g.goType(t.Elem, hint, line, prop, false)
		if !ok {
			return "", false
		}
		return "map[string]" + el, true
	case tsprops.Object:
		return g.object(t, hint, line, prop)
	case tsprops.Union:
		var rest []*tsprops.Type
		for _, u := range t.Union {
			if u.Kind != tsprops.Null && u.Kind != tsprops.Undefined {
				rest = append(rest, u)
			}
		}
		switch {
		case len(rest) == 0:
			return "any", true
		case len(rest) == 1:
			if t.Name != "" && rest[0].Kind == tsprops.StrLit {
				rest[0] = &tsprops.Type{Kind: tsprops.Union, Union: rest, Name: t.Name}
			}
			return g.goType(rest[0], hint, line, prop, top)
		}
		all := func(k ...tsprops.Kind) bool {
			for _, u := range rest {
				if !kindIn(u.Kind, k) {
					return false
				}
			}
			return true
		}
		switch {
		case all(tsprops.StrLit):
			return g.enum(&tsprops.Type{Kind: tsprops.Union, Union: rest, Name: t.Name}, hint)
		case all(tsprops.StrLit, tsprops.String):
			return "string", true
		case all(tsprops.NumLit, tsprops.Number):
			return "float64", true
		case all(tsprops.BoolLit, tsprops.Boolean):
			return "bool", true
		}
		return unsupported("the union " + t.String())
	case tsprops.Func:
		if !top {
			return unsupported("the function type " + t.String() + " (a function is a prop of its own, not inside another type)")
		}
		var ps []string
		for _, p := range t.Params {
			pt, ok := g.goType(p.Type, hint+goField(p.Name), line, prop, false)
			if !ok {
				return "", false
			}
			if p.Rest {
				pt = "..." + pt
			}
			ps = append(ps, goParam(p.Name)+" "+pt)
		}
		return "func(" + strings.Join(ps, ", ") + ")", true
	case tsprops.Node:
		return unsupported(t.String() + " (only the children prop is a slot)")
	case tsprops.Tuple:
		return unsupported("the tuple " + t.String())
	}
	return unsupported(t.String())
}

func kindIn(k tsprops.Kind, ks []tsprops.Kind) bool {
	for _, x := range ks {
		if k == x {
			return true
		}
	}
	return false
}

// name is a free Go type name: the TS name prefixed by the island's, else
// the hint.
func (g *islandGen) name(ts, hint string) string {
	n := hint
	if ts != "" {
		n = ts
		if !strings.HasPrefix(ts, g.island) {
			n = g.island + goField(ts)
		}
	}
	base := n
	for i := 2; g.names[n]; i++ {
		n = base + strconv.Itoa(i)
	}
	g.names[n] = true
	return n
}

func (g *islandGen) object(t *tsprops.Type, hint string, line int, prop string) (string, bool) {
	if t.Name != "" {
		if n, ok := g.byTS[t.Name]; ok {
			return n, true
		}
	}
	n := g.name(t.Name, hint)
	if t.Name != "" {
		g.byTS[t.Name] = n
	}
	var b strings.Builder
	b.WriteString("\ntype " + n + " struct {\n")
	seen := map[string]bool{}
	for _, f := range t.Fields {
		if f.Name == "[index]" || f.Name == "(call)" {
			g.errf(line, "prop %s: %s mixes named members with an index or call signature", prop, t.String())
			return "", false
		}
		gn := goField(f.Name)
		if gn == "" || seen[gn] {
			g.errf(line, "prop %s: member %s of %s has no Go name of its own", prop, f.Name, t.String())
			return "", false
		}
		seen[gn] = true
		ft, ok := g.goType(f.Type, n+gn, line, prop, false)
		if !ok {
			return "", false
		}
		tag := f.Name
		if f.Optional || nullable(f.Type) {
			ft = pointerFor(ft)
			tag += ",omitempty"
		}
		fmt.Fprintf(&b, "\t%s %s `json:%s`\n", gn, ft, strconv.Quote(tag))
	}
	b.WriteString("}\n")
	g.out.WriteString(b.String())
	return n, true
}

// enum writes a union of string literals as a named string type with a
// constant per value.
func (g *islandGen) enum(t *tsprops.Type, hint string) (string, bool) {
	if t.Name != "" {
		if n, ok := g.byTS[t.Name]; ok {
			return n, true
		}
	}
	n := g.name(t.Name, hint)
	if t.Name != "" {
		g.byTS[t.Name] = n
	}
	var vals []string
	g.out.WriteString("\ntype " + n + " string\n\nconst (\n")
	used := map[string]bool{}
	for _, u := range t.Union {
		c := n + goField(u.Lit)
		if goField(u.Lit) == "" || used[c] {
			c = n + "_" + strconv.Itoa(len(vals))
		}
		used[c] = true
		vals = append(vals, u.Lit)
		fmt.Fprintf(&g.out, "\t%s %s = %s\n", c, n, strconv.Quote(u.Lit))
	}
	g.out.WriteString(")\n")
	g.enums[n] = vals
	return n, true
}

// goField is a prop or member name as an exported Go identifier: onSelect →
// OnSelect, aria-label → AriaLabel, 2x → X2x.
func goField(name string) string {
	var b strings.Builder
	up := true
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' {
			up = true
			continue
		}
		if up {
			b.WriteRune(unicode.ToUpper(r))
			up = false
			continue
		}
		b.WriteRune(r)
	}
	s := b.String()
	if s != "" && unicode.IsDigit(rune(s[0])) {
		s = "X" + s
	}
	return s
}

// goParam is a parameter name as a Go identifier.
func goParam(name string) string {
	s := goField(name)
	if s == "" {
		return "_"
	}
	s = strings.ToLower(s[:1]) + s[1:]
	if token.IsKeyword(s) {
		s += "_"
	}
	return s
}

func readFile(p string) ([]byte, error) { return os.ReadFile(p) }

var liveEmbedRe = regexp.MustCompile(`(?m)^\s*(\w+)\.Live\s*(?://.*)?$`)

// islandDecl is an @<q>.Island decorator on a type: the package name it is
// written with, the type's name and offset, and the offset of the { of its
// struct (-1 when it isn't a struct).
type islandDecl struct {
	q, name       string
	nameOff, open int
}

// islandDecls are the @<q>.Island decorators of src on types, their
// arguments any expressions (@ui.Island(ui.Layout(layouts.Page("x")))),
// other decorators and comments between them and the type.
func islandDecls(src []byte) []islandDecl {
	var out []islandDecl
	for i := 0; i < len(src); i++ {
		if src[i] != '@' {
			continue
		}
		q, j := identAt(src, i+1)
		if q == "" || j >= len(src) || src[j] != '.' {
			continue
		}
		name, j := identAt(src, j+1)
		if name != "Island" {
			continue
		}
		j = skipSpace(src, j)
		if j < len(src) && src[j] == '(' {
			if j = skipParens(src, j); j < 0 {
				continue
			}
		}
		for {
			j = skipSpace(src, j)
			if bytes.HasPrefix(src[j:], []byte("//")) {
				for j < len(src) && src[j] != '\n' {
					j++
				}
				continue
			}
			if j < len(src) && src[j] == '@' {
				_, k := identAt(src, j+1)
				for k < len(src) && src[k] == '.' {
					_, k = identAt(src, k+1)
				}
				if k < len(src) && src[k] == '(' {
					if k = skipParens(src, k); k < 0 {
						break
					}
				}
				j = k
				continue
			}
			break
		}
		kw, j := identAt(src, j)
		if kw != "type" {
			continue
		}
		nameOff := skipSpace(src, j)
		name, j = identAt(src, nameOff)
		if name == "" {
			continue
		}
		d := islandDecl{q: q, name: name, nameOff: nameOff, open: -1}
		if kw, k := identAt(src, skipSpace(src, j)); kw == "struct" {
			if k = skipSpace(src, k); k < len(src) && src[k] == '{' {
				d.open = k
			}
		}
		out = append(out, d)
		i = j - 1
	}
	return out
}

// identAt is the identifier at src[i:] and the offset past it.
func identAt(src []byte, i int) (string, int) {
	j := i
	for j < len(src) && (src[j] == '_' || src[j] >= 'a' && src[j] <= 'z' || src[j] >= 'A' && src[j] <= 'Z' || j > i && src[j] >= '0' && src[j] <= '9') {
		j++
	}
	return string(src[i:j]), j
}

func skipSpace(src []byte, i int) int {
	for i < len(src) && (src[i] == ' ' || src[i] == '\t' || src[i] == '\n' || src[i] == '\r') {
		i++
	}
	return i
}

// skipParens is the offset past the ) closing the ( at open, strings and
// runes skipped; -1 when it isn't closed.
func skipParens(src []byte, open int) int {
	depth := 0
	for i := open; i < len(src); i++ {
		switch c := src[i]; c {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth--; depth == 0 {
				return i + 1
			}
		case '"', '\'', '`':
			for i++; i < len(src) && src[i] != c; i++ {
				if src[i] == '\\' && c != '`' {
					i++
				}
			}
		}
	}
	return -1
}

// islandPages writes a Render method for each @ui.Island struct of the file
// that embeds ui.Live and has none: a live page backed by its component,
// whose markup is the island (ui.IslandPage) — its exported fields the
// props, its exported methods what the component calls.
func (e *engine) islandPages(f *fileState, files []File) {
	for _, d := range islandDecls(f.src) {
		if d.open < 0 {
			continue
		}
		q, name, open := d.q, d.name, d.open
		end := matchBrace(f.src, open)
		if end < 0 {
			continue
		}
		body := f.src[open:end]
		live := false
		for _, lm := range liveEmbedRe.FindAllSubmatch(body, -1) {
			if string(lm[1]) == q {
				live = true
			}
		}
		if !live || hasMethod(files, name, "Render") {
			continue
		}
		off := d.nameOff
		var w genWriter
		w.gen("\n", off)
		if !f.bare {
			w.gen(lineDirective(f.at(off)), off)
		}
		x := recvOf(files, name)
		w.gen("func ("+x+" *"+name+") Render() "+q+".Node { return "+q+".IslandPage("+x+") }\n", off)
		f.islandGen.append(&w)
	}
}

// matchBrace is the offset of the } closing the { at open, or -1.
func matchBrace(src []byte, open int) int {
	depth := 0
	for i := open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// hasMethod reports whether the package's sources declare a method name of
// type typ.
// recvOf is the receiver name typ's methods use, else its initial.
func recvOf(files []File, typ string) string {
	re := regexp.MustCompile(`func\s*\(\s*(\w+)\s+\*?\s*` + regexp.QuoteMeta(typ) + `\s*\)`)
	for _, f := range files {
		if m := re.FindSubmatch(f.Src); m != nil && string(m[1]) != "_" {
			return string(m[1])
		}
	}
	return strings.ToLower(typ[:1])
}

func hasMethod(files []File, typ, name string) bool {
	re := regexp.MustCompile(`func\s*\(\s*\w*\s*\*?\s*` + regexp.QuoteMeta(typ) + `\s*\)\s*` + regexp.QuoteMeta(name) + `\s*\(`)
	for _, f := range files {
		if re.Match(f.Src) {
			return true
		}
	}
	return false
}
