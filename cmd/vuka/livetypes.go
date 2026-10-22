package main

import (
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// tsTypes maps the Go types a page's fields and method parameters name to
// TypeScript for vuka-live.d.ts, reading the module's sources: a struct of
// any package of the module becomes an interface (generic when it is), its
// fields as encoding/json writes them (json names, omitempty and pointers
// optional, embedded structs' fields promoted), named at every depth; other
// named types are their underlying type.
type tsTypes struct {
	root, modPath string
	pkgs          map[string]*goPkg
	names         map[*goDecl]string
	taken         map[string]*goDecl
	want          []*goDecl
	inlining      map[*goDecl]bool
}

type goPkg struct {
	dir, name string
	decls     map[string]*goDecl
}

// goDecl is a type declaration: its expression is toks[start:end].
type goDecl struct {
	pkg        *goPkg
	name       string
	toks       []wtok
	start, end int
	tparams    []string
	imports    map[string]string
}

// tsScope is where a type expression is read: its package and file imports,
// and what its type parameters stand for.
type tsScope struct {
	pkg     *goPkg
	imports map[string]string
	subst   map[string]string
}

type tsField struct {
	name     string
	ts       string
	optional bool
}

func newTSTypes(root, modPath string) *tsTypes {
	return &tsTypes{root: root, modPath: modPath, pkgs: map[string]*goPkg{}, names: map[*goDecl]string{},
		taken: map[string]*goDecl{}, inlining: map[*goDecl]bool{}}
}

func (r *tsTypes) pkg(dir string) *goPkg {
	if p, ok := r.pkgs[dir]; ok {
		return p
	}
	p := &goPkg{dir: dir, name: filepath.Base(dir), decls: map[string]*goDecl{}}
	r.pkgs[dir] = p
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || strings.Contains(n, "_test.") || strings.HasSuffix(n, "_vuka.go") || strings.HasSuffix(n, "_html.go") {
			continue
		}
		src, ok := goSource(filepath.Join(dir, n))
		if !ok {
			continue
		}
		full, _ := scanFile(n, src)
		imports := fileImports(dropNewlineSemis(full))
		if len(full) > 1 && full[0].tok == token.PACKAGE && full[1].tok == token.IDENT {
			p.name = full[1].lit
		}
		readDecls(p, full, imports)
	}
	return p
}

// readDecls records a file's top-level type declarations.
func readDecls(p *goPkg, full []wtok, imports map[string]string) {
	depth := 0
	for i := 0; i < len(full); i++ {
		switch full[i].tok {
		case token.LBRACE, token.LPAREN, token.LBRACK:
			depth++
			continue
		case token.RBRACE, token.RPAREN, token.RBRACK:
			depth--
			continue
		}
		if depth != 0 || full[i].tok != token.TYPE || i+1 >= len(full) {
			continue
		}
		if full[i+1].tok != token.LPAREN {
			if end := typeSpec(p, full, i+1, imports); end > i {
				i = end - 1
			}
			continue
		}
		j := i + 2
		for j < len(full) && full[j].tok != token.RPAREN {
			if full[j].tok == token.IDENT {
				end := typeSpec(p, full, j, imports)
				if end <= j {
					break
				}
				j = end
				continue
			}
			j++
		}
		i = j
	}
}

// typeSpec reads the spec whose name is at i, returning the index after it.
func typeSpec(p *goPkg, full []wtok, i int, imports map[string]string) int {
	if full[i].tok != token.IDENT {
		return i
	}
	d := &goDecl{pkg: p, name: full[i].lit, toks: full, imports: imports}
	k := i + 1
	if k+2 < len(full) && full[k].tok == token.LBRACK && full[k+1].tok == token.IDENT && full[k+2].tok != token.RBRACK {
		end := matchParen(full, k)
		if end < 0 {
			return i
		}
		seg := true
		depth := 0
		for j := k + 1; j < end; j++ {
			switch full[j].tok {
			case token.LBRACK, token.LPAREN, token.LBRACE:
				depth++
			case token.RBRACK, token.RPAREN, token.RBRACE:
				depth--
			case token.COMMA:
				if depth == 0 {
					seg = true
				}
				continue
			case token.IDENT:
				if seg && depth == 0 {
					d.tparams = append(d.tparams, full[j].lit)
				}
			}
			seg = false
		}
		k = end + 1
	}
	if k < len(full) && full[k].tok == token.ASSIGN {
		k++
	}
	d.start = k
	depth := 0
	for k < len(full) {
		switch full[k].tok {
		case token.LBRACE, token.LPAREN, token.LBRACK:
			depth++
		case token.RBRACE, token.RPAREN, token.RBRACK:
			if depth == 0 {
				d.end = k
				p.decls[d.name] = d
				return k
			}
			depth--
		case token.SEMICOLON:
			if depth == 0 {
				d.end = k
				p.decls[d.name] = d
				return k + 1
			}
		}
		k++
	}
	d.end = k
	p.decls[d.name] = d
	return k
}

func (d *goDecl) expr() []wtok { return d.toks[d.start:d.end] }

func (d *goDecl) isStruct() bool {
	e := d.expr()
	return len(e) > 1 && e[0].tok == token.STRUCT && e[1].tok == token.LBRACE
}

// scope is where d's own expression is read; its type parameters stand for
// themselves (a generic interface) unless args are given.
func (d *goDecl) scope(args []string) *tsScope {
	s := &tsScope{pkg: d.pkg, imports: d.imports, subst: map[string]string{}}
	for i, tp := range d.tparams {
		s.subst[tp] = tp
		if i < len(args) {
			s.subst[tp] = args[i]
		}
	}
	return s
}

// claim names d's interface name, an island's own when it is one.
func (r *tsTypes) claim(d *goDecl, name string) {
	r.names[d], r.taken[name] = name, d
}

// nameOf is the interface name of the struct d, queued for writing the first
// time.
func (r *tsTypes) nameOf(d *goDecl) string {
	if n, ok := r.names[d]; ok {
		return n
	}
	n := d.name
	if o := r.taken[n]; o != nil && o != d {
		n = strings.ToUpper(d.pkg.name[:1]) + d.pkg.name[1:] + d.name
		for i := 2; r.taken[n] != nil; i++ {
			n = d.pkg.name + d.name + strconv.Itoa(i)
		}
	}
	r.claim(d, n)
	r.want = append(r.want, d)
	return n
}

// lookup finds the declaration a (possibly qualified) name refers to.
func (r *tsTypes) lookup(s *tsScope, qual, name string) *goDecl {
	p := s.pkg
	if qual != "" {
		ip, ok := s.imports[qual]
		if !ok || r.modPath == "" || (ip != r.modPath && !strings.HasPrefix(ip, r.modPath+"/")) {
			return nil
		}
		p = r.pkg(filepath.Join(r.root, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(ip, r.modPath), "/"))))
	}
	if p == nil {
		return nil
	}
	return p.decls[name]
}

// ts is the TypeScript of the type expression t, read in s.
func (r *tsTypes) ts(t []wtok, s *tsScope) string {
	if len(t) == 0 {
		return "unknown"
	}
	switch t[0].tok {
	case token.MUL:
		return r.ts(t[1:], s) + " | null"
	case token.ELLIPSIS:
		return arrayOf(r.ts(t[1:], s))
	case token.LBRACK:
		end := matchParen(t, 0)
		if end < 0 {
			return "unknown"
		}
		elem := t[end+1:]
		if len(elem) == 1 && elem[0].tok == token.IDENT && (elem[0].lit == "byte" || elem[0].lit == "uint8") {
			return "string"
		}
		return arrayOf(r.ts(elem, s))
	case token.MAP:
		if len(t) < 2 || t[1].tok != token.LBRACK {
			return "unknown"
		}
		end := matchParen(t, 1)
		if end < 0 {
			return "unknown"
		}
		key := "string"
		if r.ts(t[2:end], s) == "number" {
			key = "number"
		}
		return "Record<" + key + ", " + r.ts(t[end+1:], s) + ">"
	case token.STRUCT:
		if len(t) < 2 || t[1].tok != token.LBRACE {
			return "unknown"
		}
		var b strings.Builder
		b.WriteString("{ ")
		for _, f := range r.structFields(t, 1, s, map[*goDecl]bool{}) {
			b.WriteString(fieldTS(f) + " ")
		}
		b.WriteString("}")
		return b.String()
	case token.IDENT:
	default:
		return "unknown"
	}
	qual, name, rest := "", t[0].lit, t[1:]
	if len(rest) >= 2 && rest[0].tok == token.PERIOD && rest[1].tok == token.IDENT {
		qual, name, rest = name, rest[1].lit, rest[2:]
	}
	var args []string
	if len(rest) > 0 && rest[0].tok == token.LBRACK {
		end := matchParen(rest, 0)
		if end < 0 {
			return "unknown"
		}
		for _, a := range callArgsBracket(rest, end) {
			args = append(args, r.ts(a, s))
		}
		rest = rest[end+1:]
	}
	if len(rest) > 0 {
		return "unknown"
	}
	if qual == "" {
		if v, ok := s.subst[name]; ok {
			return v
		}
		if ts := basicTS(name); ts != "unknown" {
			return ts
		}
	}
	switch qual + "." + name {
	case "time.Time":
		return "string"
	case "time.Duration":
		return "number"
	}
	d := r.lookup(s, qual, name)
	if d == nil {
		return "unknown"
	}
	return r.named(d, args)
}

// named is the TypeScript of a use of d: a struct's interface, else d's
// underlying type.
func (r *tsTypes) named(d *goDecl, args []string) string {
	if d.isStruct() {
		n := r.nameOf(d)
		if len(d.tparams) == 0 {
			return n
		}
		full := make([]string, len(d.tparams))
		for i := range full {
			full[i] = "unknown"
			if i < len(args) {
				full[i] = args[i]
			}
		}
		return n + "<" + strings.Join(full, ", ") + ">"
	}
	if r.inlining[d] {
		return "unknown"
	}
	r.inlining[d] = true
	defer delete(r.inlining, d)
	return r.ts(d.expr(), d.scope(args))
}

// callArgsBracket splits the type arguments inside the brackets t[0] … t[end].
func callArgsBracket(t []wtok, end int) [][]wtok {
	var out [][]wtok
	depth, start := 0, 1
	for j := 1; j < end; j++ {
		switch t[j].tok {
		case token.LBRACK, token.LPAREN, token.LBRACE:
			depth++
		case token.RBRACK, token.RPAREN, token.RBRACE:
			depth--
		case token.COMMA:
			if depth == 0 {
				out = append(out, t[start:j])
				start = j + 1
			}
		}
	}
	if end > start {
		out = append(out, t[start:end])
	}
	return out
}

func arrayOf(ts string) string {
	if strings.Contains(ts, " | ") {
		return "(" + ts + ")[]"
	}
	return ts + "[]"
}

func fieldTS(f tsField) string {
	opt := ""
	if f.optional {
		opt = "?"
	}
	return tsKey(f.name) + opt + ": " + f.ts + ";"
}

// declFields are the fields of the struct d as JSON has them.
func (r *tsTypes) declFields(d *goDecl) []tsField {
	return r.structFields(d.toks, d.start+1, d.scope(nil), map[*goDecl]bool{d: true})
}

// structFields reads the struct whose { is at t[open], following
// encoding/json: exported fields under their json names, json:"-" left out,
// pointers and omitempty optional, an untagged embedded struct's fields
// promoted (the shallowest wins; at one depth a tagged one, else neither).
func (r *tsTypes) structFields(t []wtok, open int, s *tsScope, seen map[*goDecl]bool) []tsField {
	type cand struct {
		f      tsField
		depth  int
		tagged bool
	}
	var cands []cand
	var walk func(t []wtok, open int, s *tsScope, depth int, seen map[*goDecl]bool)
	walk = func(t []wtok, open int, s *tsScope, depth int, seen map[*goDecl]bool) {
		for _, fl := range structBodyFields(t, open) {
			fl, _ = cutAttrs(fl)
			tag := ""
			if n := len(fl); n > 0 && fl[n-1].tok == token.STRING {
				if v, err := strconv.Unquote(fl[n-1].lit); err == nil {
					tag = v
				}
				fl = fl[:n-1]
			}
			jname, opts, _ := strings.Cut(reflect.StructTag(tag).Get("json"), ",")
			if jname == "-" && opts == "" {
				continue
			}
			omit := strings.Contains(","+opts+",", ",omitempty,") || strings.Contains(","+opts+",", ",omitzero,")
			names, typ := splitNamesType(fl)
			if len(names) == 0 { // embedded
				et := typ
				ptr := len(et) > 0 && et[0].tok == token.MUL
				if ptr {
					et = et[1:]
				}
				tname := ""
				for _, w := range et {
					if w.tok == token.LBRACK {
						break
					}
					if w.tok == token.IDENT {
						tname = w.lit
					}
				}
				if tname == "" {
					continue
				}
				if jname == "" {
					if d, args := r.embeddedDecl(et, s); d != nil && d.isStruct() {
						if !seen[d] {
							next := map[*goDecl]bool{d: true}
							for k := range seen {
								next[k] = true
							}
							walk(d.toks, d.start+1, d.scope(args), depth+1, next)
						}
						continue
					}
				}
				if !token.IsExported(tname) {
					continue
				}
				names = []string{tname}
			}
			if len(names) > 1 {
				jname = ""
			}
			for _, nm := range names {
				if !token.IsExported(nm) {
					continue
				}
				wire := nm
				if jname != "" {
					wire = jname
				}
				opt := omit || len(typ) > 0 && typ[0].tok == token.MUL
				cands = append(cands, cand{tsField{wire, r.ts(typ, s), opt}, depth, jname != ""})
			}
		}
	}
	walk(t, open, s, 0, seen)
	win := map[string]int{}
	for i, c := range cands {
		w, ok := win[c.f.name]
		at := w
		if w < 0 {
			at = -1 - w
		}
		switch {
		case !ok || c.depth < cands[at].depth:
			win[c.f.name] = i
		case w >= 0 && c.depth == cands[w].depth:
			switch {
			case c.tagged && !cands[w].tagged:
				win[c.f.name] = i
			case c.tagged == cands[w].tagged:
				win[c.f.name] = -1 - w // a tie at this depth: neither, unless a shallower one comes
			}
		case w < 0 && c.depth == cands[-1-w].depth && c.tagged && !cands[-1-w].tagged:
			win[c.f.name] = i
		}
	}
	var out []tsField
	for i, c := range cands {
		if win[c.f.name] == i {
			out = append(out, c.f)
		}
	}
	return out
}

// embeddedDecl is the declaration an embedded field's type names, and its
// type arguments.
func (r *tsTypes) embeddedDecl(t []wtok, s *tsScope) (*goDecl, []string) {
	if len(t) == 0 || t[0].tok != token.IDENT {
		return nil, nil
	}
	qual, name, rest := "", t[0].lit, t[1:]
	if len(rest) >= 2 && rest[0].tok == token.PERIOD && rest[1].tok == token.IDENT {
		qual, name, rest = name, rest[1].lit, rest[2:]
	}
	var args []string
	if len(rest) > 0 && rest[0].tok == token.LBRACK {
		if end := matchParen(rest, 0); end > 0 {
			for _, a := range callArgsBracket(rest, end) {
				args = append(args, r.ts(a, s))
			}
		}
	}
	return r.lookup(s, qual, name), args
}

// structBodyFields are the field token lists of the struct whose { is at
// t[open], split at the semicolons between fields.
func structBodyFields(t []wtok, open int) [][]wtok {
	var out [][]wtok
	depth, start := 0, open+1
	for i := open; i < len(t); i++ {
		switch t[i].tok {
		case token.LBRACE, token.LPAREN, token.LBRACK:
			depth++
		case token.RBRACE, token.RPAREN, token.RBRACK:
			depth--
			if depth == 0 {
				if i > start {
					out = append(out, t[start:i])
				}
				return out
			}
		case token.SEMICOLON:
			if depth == 1 {
				if i > start {
					out = append(out, t[start:i])
				}
				start = i + 1
			}
		}
	}
	return out
}

// goSource is the Go of a module source file: a .go or .vuka file's text,
// a .vuka.html file's header (read in place of its x_html.go); false for
// any other file.
func goSource(file string) ([]byte, bool) {
	if !strings.HasSuffix(file, ".go") && !strings.HasSuffix(file, ".vuka") && !strings.HasSuffix(file, ".vuka.html") {
		return nil, false
	}
	src, err := os.ReadFile(file)
	if err != nil {
		return nil, false
	}
	if !strings.HasSuffix(file, ".vuka.html") {
		return src, true
	}
	sh, _ := transpile.ParseShape(src)
	if sh == nil {
		return nil, false
	}
	lines := strings.SplitAfter(string(src), "\n")
	return []byte(strings.Join(lines[:min(sh.Markup, len(lines))], "")), true
}
