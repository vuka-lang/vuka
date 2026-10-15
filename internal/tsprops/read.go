package tsprops

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Component is what a component file declares of its props.
type Component struct {
	Framework string  // vue, react, svelte or vanilla
	Props     []*Prop // in declaration order
	// Children: the component takes children (a Vue default <slot>, a React
	// children prop, a Svelte children snippet).
	Children bool
}

// Prop is one prop of a component.
type Prop struct {
	Name     string
	Type     *Type
	Optional bool // ?, a default (withDefaults, a destructuring default)
	Model    bool // written back by the component: Vue's defineModel, Svelte's $bindable
	Event    bool // from Vue's defineEmits: an on<Name> listener
	Line     int  // where it is declared in the component file, 1-based
}

// Error is a problem reading a component's props, at a line of its file.
type Error struct {
	Line int
	Msg  string
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%d: %s", e.Line, e.Msg)
	}
	return e.Msg
}

// FrameworkOf is the framework a component file's extension names: .vue,
// .tsx/.jsx (React), .svelte, else vanilla (.ts/.js).
func FrameworkOf(file string) string {
	switch strings.ToLower(path.Ext(file)) {
	case ".vue":
		return "vue"
	case ".tsx", ".jsx":
		return "react"
	case ".svelte":
		return "svelte"
	}
	return "vanilla"
}

// IsComponent reports whether an import path names a client component file.
func IsComponent(p string) bool {
	switch strings.ToLower(path.Ext(p)) {
	case ".vue", ".tsx", ".jsx", ".svelte", ".ts", ".js", ".mts", ".mjs":
		return strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../")
	}
	return false
}

// Read reads the props a component file declares. file is its path (for
// its extension and its relative imports), readFile reads another file —
// a type imported from a relative path is followed one level.
func Read(file string, src []byte, readFile func(string) ([]byte, error)) (*Component, error) {
	fw := FrameworkOf(file)
	text := string(src)
	r := &reader{file: file, src: text, readFile: readFile, fw: fw}
	switch fw {
	case "vue", "svelte":
		r.script = scriptsOnly(text)
	default:
		r.script = text
	}
	r.toks = lex(r.script)
	r.decls = map[string]*decl{}
	r.imports = map[string]importRef{}
	r.collect()
	c := &Component{Framework: fw}
	var err error
	switch fw {
	case "vue":
		err = r.vue(c)
	case "react":
		err = r.react(c)
	case "svelte":
		err = r.svelte(c)
	default:
		err = r.vanilla(c)
	}
	if err != nil {
		return c, err
	}
	for _, p := range c.Props {
		p.Type = r.resolve(p.Type, 0)
	}
	if fw != "vue" { // a children prop is the slot
		kept := c.Props[:0]
		for _, p := range c.Props {
			if p.Name == "children" && (p.Type.Kind == Node || p.Type.Kind == Any) {
				c.Children = true
				continue
			}
			kept = append(kept, p)
		}
		c.Props = kept
	}
	return c, nil
}

var scriptRe = regexp.MustCompile(`(?is)<script\b[^>]*>(.*?)</script\s*>`)

// scriptsOnly is an SFC's text with everything outside its <script> blocks
// blanked (newlines kept), so offsets are the file's.
func scriptsOnly(src string) string {
	b := []byte(src)
	keep := make([]bool, len(b))
	for _, m := range scriptRe.FindAllStringSubmatchIndex(src, -1) {
		for i := m[2]; i < m[3]; i++ {
			keep[i] = true
		}
	}
	for i := range b {
		if !keep[i] && b[i] != '\n' {
			b[i] = ' '
		}
	}
	return string(b)
}

type decl struct {
	name    string
	typ     *Type // the parsed type (an alias's, an interface's object)
	extends []*Type
	params  []string // type parameters
	off     int
}

type importRef struct {
	path, name string
}

type reader struct {
	file, src, script string
	fw                string
	readFile          func(string) ([]byte, error)
	toks              []token
	decls             map[string]*decl
	imports           map[string]importRef
	nested            bool // reading an imported file: its imports aren't followed
	defaults          map[string]bool // the props a component's destructuring gives defaults
}

func (r *reader) line(off int) int {
	return strings.Count(r.src[:min(off, len(r.src))], "\n") + 1
}

func (r *reader) errAt(off int, format string, args ...any) error {
	return &Error{Line: r.line(off), Msg: fmt.Sprintf(format, args...)}
}

func (r *reader) parserAt(i int) *parser { return &parser{toks: r.toks, src: r.script, i: i} }

func (r *reader) tok(i int) token { return r.toks[min(i, len(r.toks)-1)] }

func (r *reader) isPunct(i int, s string) bool {
	t := r.tok(i)
	return t.kind == tPunct && t.text == s
}

func (r *reader) isIdent(i int, s string) bool {
	t := r.tok(i)
	return t.kind == tIdent && t.text == s
}

// collect reads the file's interfaces, type aliases and imports.
func (r *reader) collect() {
	for i := 0; i < len(r.toks); i++ {
		t := r.toks[i]
		if t.kind != tIdent || i > 0 && r.isPunct(i-1, ".") {
			continue
		}
		switch t.text {
		case "interface":
			if r.tok(i+1).kind != tIdent {
				continue
			}
			d := &decl{name: r.tok(i + 1).text, off: t.off}
			j := i + 2
			if r.isPunct(j, "<") {
				end := matching(r.toks, j)
				if end < 0 {
					continue
				}
				d.params = typeParams(r.toks[j+1 : end])
				j = end + 1
			}
			if r.isIdent(j, "extends") {
				p := r.parserAt(j + 1)
				for {
					d.extends = append(d.extends, p.postfix())
					if p.err != nil || !p.eat(",") {
						break
					}
				}
				j = p.i
			}
			if !r.isPunct(j, "{") {
				continue
			}
			p := r.parserAt(j)
			d.typ = p.object()
			if p.err != nil {
				d.typ = &Type{Kind: Unknown, Text: d.name}
			}
			d.typ.Name = d.name
			if _, dup := r.decls[d.name]; !dup {
				r.decls[d.name] = d
			}
			i = p.i - 1
		case "type":
			if r.tok(i+1).kind != tIdent || !(r.isPunct(i+2, "=") || r.isPunct(i+2, "<")) {
				continue
			}
			d := &decl{name: r.tok(i + 1).text, off: t.off}
			j := i + 2
			if r.isPunct(j, "<") {
				end := matching(r.toks, j)
				if end < 0 {
					continue
				}
				d.params = typeParams(r.toks[j+1 : end])
				j = end + 1
			}
			if !r.isPunct(j, "=") {
				continue
			}
			p := r.parserAt(j + 1)
			d.typ = p.typ()
			if p.err != nil {
				d.typ = &Type{Kind: Unknown, Text: d.name}
			}
			if _, dup := r.decls[d.name]; !dup {
				r.decls[d.name] = d
			}
			i = p.i - 1
		case "import":
			r.readImport(i)
		}
	}
}

func typeParams(toks []token) []string {
	var out []string
	depth := 0
	expect := true
	for _, t := range toks {
		switch {
		case t.kind == tPunct && (t.text == "<" || t.text == "(" || t.text == "{" || t.text == "["):
			depth++
		case t.kind == tPunct && (t.text == ">" || t.text == ")" || t.text == "}" || t.text == "]"):
			depth--
		case depth == 0 && t.kind == tPunct && t.text == ",":
			expect = true
		case depth == 0 && expect && t.kind == tIdent:
			out = append(out, t.text)
			expect = false
		}
	}
	return out
}

// readImport records the names `import { A, B as C } from './x'` (or import
// type …) brings in from a relative path.
func (r *reader) readImport(i int) {
	j := i + 1
	if r.isIdent(j, "type") {
		j++
	}
	if !r.isPunct(j, "{") {
		return
	}
	end := matching(r.toks, j)
	if end < 0 || !r.isIdent(end+1, "from") || r.tok(end+2).kind != tString {
		return
	}
	from := r.tok(end + 2).text
	if !strings.HasPrefix(from, "./") && !strings.HasPrefix(from, "../") {
		return
	}
	for k := j + 1; k < end; k++ {
		if r.isIdent(k, "type") && r.tok(k+1).kind == tIdent {
			k++
		}
		if r.tok(k).kind != tIdent {
			continue
		}
		name, local := r.tok(k).text, r.tok(k).text
		if r.isIdent(k+1, "as") && r.tok(k+2).kind == tIdent {
			local = r.tok(k + 2).text
			k += 2
		}
		r.imports[local] = importRef{from, name}
		for k+1 < end && !r.isPunct(k+1, ",") {
			k++
		}
	}
}

// lookup is the declaration of a name: the file's own, else one a relative
// import brings in (followed one level).
func (r *reader) lookup(name string) (*decl, *reader) {
	if d, ok := r.decls[name]; ok {
		return d, r
	}
	ref, ok := r.imports[name]
	if !ok || r.nested || r.readFile == nil {
		return nil, nil
	}
	base := path.Join(path.Dir(r.file), ref.path)
	for _, cand := range []string{base, base + ".ts", base + ".d.ts", base + ".tsx", base + "/index.ts", base + ".js"} {
		if path.Ext(cand) == "" {
			continue
		}
		src, err := r.readFile(cand)
		if err != nil {
			continue
		}
		o := &reader{file: cand, src: string(src), script: string(src), readFile: r.readFile, nested: true}
		o.toks = lex(o.script)
		o.decls = map[string]*decl{}
		o.imports = map[string]importRef{}
		o.collect()
		if d, ok := o.decls[ref.name]; ok {
			return d, o
		}
		return nil, nil
	}
	return nil, nil
}

// resolve replaces the references in t by what they name, as far as props
// need: local and imported interfaces and aliases, and the built-in generic
// types props use.
func (r *reader) resolve(t *Type, depth int) *Type {
	if t == nil {
		return &Type{Kind: Unknown}
	}
	if depth > 12 {
		return &Type{Kind: Unknown, Text: t.String() + " (too deep or recursive)"}
	}
	switch t.Kind {
	case Union, Tuple:
		out := *t
		out.Union = nil
		for _, u := range t.Union {
			out.Union = append(out.Union, r.resolve(u, depth+1))
		}
		return &out
	case Array, Record:
		out := *t
		out.Elem = r.resolve(t.Elem, depth+1)
		return &out
	case Object:
		out := *t
		out.Fields = nil
		for _, f := range t.Fields {
			nf := *f
			nf.Type = r.resolve(f.Type, depth+1)
			out.Fields = append(out.Fields, &nf)
		}
		return &out
	case Func:
		out := *t
		out.Params = nil
		for _, p := range t.Params {
			np := *p
			np.Type = r.resolve(p.Type, depth+1)
			out.Params = append(out.Params, &np)
		}
		return &out
	case Ref:
		return r.resolveRef(t, depth)
	}
	return t
}

func (r *reader) resolveRef(t *Type, depth int) *Type {
	arg := func(i int) *Type {
		if i < len(t.Args) {
			return r.resolve(t.Args[i], depth+1)
		}
		return &Type{Kind: Any, Text: "any"}
	}
	switch t.Name {
	case "&":
		merged := &Type{Kind: Object, Text: t.Text}
		for _, a := range t.Args {
			o := r.resolve(a, depth+1)
			if o.Kind != Object {
				return &Type{Kind: Unknown, Text: t.Text}
			}
			merged.Fields = append(merged.Fields, o.Fields...)
		}
		return merged
	case "Array", "ReadonlyArray":
		return &Type{Kind: Array, Elem: arg(0), Text: t.Text}
	case "Record":
		k := arg(0)
		if k.Kind != String {
			return &Type{Kind: Unknown, Text: t.Text}
		}
		return &Type{Kind: Record, Elem: arg(1), Text: t.Text}
	case "Partial", "Required", "Readonly":
		o := arg(0)
		if o.Kind != Object {
			return &Type{Kind: Unknown, Text: t.Text}
		}
		out := *o
		out.Fields = nil
		for _, f := range o.Fields {
			nf := *f
			switch t.Name {
			case "Partial":
				nf.Optional = true
			case "Required":
				nf.Optional = false
			}
			out.Fields = append(out.Fields, &nf)
		}
		out.Name, out.Text = "", t.Text
		return &out
	case "Date":
		return &Type{Kind: Date, Text: "Date"}
	case "String":
		return &Type{Kind: String, Text: t.Text}
	case "Number":
		return &Type{Kind: Number, Text: t.Text}
	case "Boolean":
		return &Type{Kind: Boolean, Text: t.Text}
	case "Object":
		return &Type{Kind: Any, Text: t.Text}
	case "Function", "VoidFunction":
		return &Type{Kind: Func, Text: t.Text}
	case "ReactNode", "React.ReactNode", "ReactElement", "React.ReactElement", "JSX.Element", "React.JSX.Element",
		"Snippet", "VNode", "VNodeChild", "React.ReactChild", "ReactChild":
		return &Type{Kind: Node, Text: t.Text}
	case "PropsWithChildren", "React.PropsWithChildren":
		o := arg(0)
		if o.Kind != Object {
			return &Type{Kind: Unknown, Text: t.Text}
		}
		out := *o
		out.Fields = append(append([]*Field(nil), o.Fields...), &Field{Name: "children", Optional: true, Type: &Type{Kind: Node, Text: "ReactNode"}})
		return &out
	}
	d, in := r.lookup(t.Name)
	if d == nil || d.typ == nil {
		return &Type{Kind: Unknown, Text: t.Text}
	}
	body := d.typ
	if len(d.params) > 0 {
		subst := map[string]*Type{}
		for i, p := range d.params {
			if i < len(t.Args) {
				subst[p] = t.Args[i]
			}
		}
		body = substitute(body, subst)
	}
	res := in.resolve(body, depth+1)
	if len(d.extends) > 0 && res.Kind == Object {
		var fields []*Field
		for _, e := range d.extends {
			base := in.resolve(e, depth+1)
			if base.Kind != Object {
				return &Type{Kind: Unknown, Text: t.Text}
			}
			fields = append(fields, base.Fields...)
		}
		res.Fields = append(fields, res.Fields...)
	}
	if res.Kind == Object || res.Kind == Union {
		res.Name = d.name
	}
	return res
}

// substitute replaces a generic declaration's type parameters.
func substitute(t *Type, s map[string]*Type) *Type {
	if t == nil {
		return nil
	}
	if t.Kind == Ref && len(t.Args) == 0 {
		if r, ok := s[t.Name]; ok {
			return r
		}
	}
	out := *t
	out.Elem = substitute(t.Elem, s)
	out.Union, out.Args, out.Fields, out.Params = nil, nil, nil, nil
	for _, u := range t.Union {
		out.Union = append(out.Union, substitute(u, s))
	}
	for _, a := range t.Args {
		out.Args = append(out.Args, substitute(a, s))
	}
	for _, f := range t.Fields {
		nf := *f
		nf.Type = substitute(f.Type, s)
		out.Fields = append(out.Fields, &nf)
	}
	for _, p := range t.Params {
		np := *p
		np.Type = substitute(p.Type, s)
		out.Params = append(out.Params, &np)
	}
	return &out
}

// propsOf turns an object type (or a reference to one) into props.
func (r *reader) propsOf(t *Type, off int) ([]*Prop, error) {
	rt := r.resolve(t, 0)
	if rt.Kind != Object {
		return nil, r.errAt(off, "the props type %s isn't an object type this reader follows (an interface or type of this file, or one imported from a relative path)", t.String())
	}
	var out []*Prop
	for _, f := range rt.Fields {
		if f.Name == "[index]" || f.Name == "(call)" {
			return nil, r.errAt(f.Off, "the props type %s has an index or call signature; props are named members", t.String())
		}
		out = append(out, &Prop{Name: f.Name, Type: f.Type, Optional: f.Optional, Line: r.line(f.Off)})
	}
	return out, nil
}

// findCall is the index of the first token sequence name ( or name < at or
// after from, or -1.
func (r *reader) findCall(from int, names ...string) (int, string) {
	for i := from; i < len(r.toks); i++ {
		t := r.toks[i]
		if t.kind != tIdent || i > 0 && r.isPunct(i-1, ".") {
			continue
		}
		for _, n := range names {
			if t.text == n && (r.isPunct(i+1, "(") || r.isPunct(i+1, "<")) {
				return i, n
			}
		}
	}
	return -1, ""
}

// typeArg reads the <T> after a call's name at i, and the index after it.
func (r *reader) typeArg(i int) (*Type, int, error) {
	if !r.isPunct(i+1, "<") {
		return nil, i + 1, nil
	}
	p := r.parserAt(i + 2)
	t := p.typ()
	if p.err != nil {
		return nil, 0, r.errAt(r.tok(i).off, "%s's type: %v", r.tok(i).text, p.err)
	}
	if !p.eat(">") {
		return nil, 0, r.errAt(r.tok(i).off, "%s's type argument isn't one type", r.tok(i).text)
	}
	return t, p.i, nil
}

// destructured reads a destructuring pattern { a, b = 1, c: d, ...rest }
// whose { is at i: each name and whether it has a default, and the default's
// first token (for $bindable).
type destructuredName struct {
	name       string
	hasDefault bool
	bindable   bool
	off        int
}

func (r *reader) destructured(i int) ([]destructuredName, int) {
	end := matching(r.toks, i)
	if end < 0 {
		return nil, -1
	}
	var out []destructuredName
	for k := i + 1; k < end; k++ {
		if r.isPunct(k, ",") {
			continue
		}
		if r.isPunct(k, "...") {
			k++
			for k+1 < end && !r.isPunct(k+1, ",") {
				k++
			}
			continue
		}
		t := r.tok(k)
		if t.kind != tIdent && t.kind != tString {
			continue
		}
		dn := destructuredName{name: t.text, off: t.off}
		k++
		if r.isPunct(k, ":") { // renamed: a: b
			k++
			if r.isPunct(k, "{") || r.isPunct(k, "[") {
				k = matching(r.toks, k) + 1
			} else {
				k++
			}
		}
		if r.isPunct(k, "=") {
			dn.hasDefault = true
			k++
			if r.isIdent(k, "$bindable") {
				dn.bindable = true
				dn.hasDefault = r.isPunct(k+1, "(") && !r.isPunct(k+2, ")")
			}
			p := r.parserAt(k)
			p.skipExpr()
			k = p.i
		}
		out = append(out, dn)
		k-- // the loop's k++ lands on the , or the end
		for k+1 < end && !r.isPunct(k+1, ",") {
			k++
		}
	}
	return out, end
}

// objectKeys is the keys of an object literal whose { is at i.
func (r *reader) objectKeys(i int) map[string]bool {
	end := matching(r.toks, i)
	keys := map[string]bool{}
	if end < 0 {
		return keys
	}
	depth := 0
	expect := true
	for k := i + 1; k < end; k++ {
		t := r.tok(k)
		if t.kind == tPunct {
			switch t.text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				depth--
			case ",":
				if depth == 0 {
					expect = true
				}
			}
			continue
		}
		if depth == 0 && expect && (t.kind == tIdent || t.kind == tString) {
			keys[t.text] = true
			expect = false
		}
	}
	return keys
}
