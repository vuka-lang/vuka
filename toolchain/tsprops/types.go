package tsprops

import (
	"fmt"
	"strings"
)

// Kind is what a TypeScript type is, as far as props go.
type Kind int

const (
	Unknown   Kind = iota // a type the reader doesn't know: Map<…>, HTMLElement, typeof x, …
	String                // string
	Number                // number
	Boolean               // boolean
	Date                  // Date
	Any                   // any, unknown, object
	Null                  // null
	Undefined             // undefined, void
	StrLit                // a string literal type: 'bar'
	NumLit                // a number literal type: 3
	BoolLit               // true, false
	Union                 // A | B
	Array                 // T[], Array<T>, ReadonlyArray<T>
	Object                // { a: T }, an interface
	Record                // Record<string, T>, { [k: string]: T }
	Func                  // (a: T) => void
	Node                  // the component's children: ReactNode, Snippet, VNode, JSX.Element
	Ref                   // a name not resolved yet
	Tuple                 // [A, B]
)

// Type is a TypeScript type read from a component.
type Type struct {
	Kind   Kind
	Lit    string   // StrLit/NumLit/BoolLit: the literal's value
	Elem   *Type    // Array's element, Record's value
	Union  []*Type  // Union's members; Tuple's elements
	Fields []*Field // Object's members
	Params []*Param // Func's parameters
	Name   string   // Ref: the name; Object, Union: the interface or alias it is
	Args   []*Type  // Ref's type arguments
	Text   string   // the type as written
}

// Field is a member of an object type.
type Field struct {
	Name     string
	Type     *Type
	Optional bool
	Off      int // where it is declared, an offset in its file
}

// Param is a parameter of a function type.
type Param struct {
	Name     string
	Type     *Type
	Optional bool
	Rest     bool
}

func (t *Type) String() string {
	if t == nil {
		return "unknown"
	}
	if t.Text != "" {
		return t.Text
	}
	switch t.Kind {
	case String:
		return "string"
	case Number:
		return "number"
	case Boolean:
		return "boolean"
	case Date:
		return "Date"
	case Any:
		return "any"
	case Null:
		return "null"
	case Undefined:
		return "undefined"
	case StrLit:
		return "'" + t.Lit + "'"
	case NumLit, BoolLit:
		return t.Lit
	case Array:
		return t.Elem.String() + "[]"
	case Union:
		var s []string
		for _, u := range t.Union {
			s = append(s, u.String())
		}
		return strings.Join(s, " | ")
	case Func:
		var s []string
		for _, p := range t.Params {
			s = append(s, p.Name+": "+p.Type.String())
		}
		return "(" + strings.Join(s, ", ") + ") => void"
	case Record:
		return "Record<string, " + t.Elem.String() + ">"
	case Ref:
		return t.Name
	}
	return "unknown"
}

// parser reads types from a token list.
type parser struct {
	toks []token
	src  string
	i    int
	err  error
}

func (p *parser) peek() token      { return p.toks[p.i] }
func (p *parser) at(k int) token   { return p.toks[min(p.i+k, len(p.toks)-1)] }
func (p *parser) next() token      { t := p.toks[p.i]; p.i = min(p.i+1, len(p.toks)-1); return t }
func (p *parser) is(s string) bool { t := p.peek(); return t.kind == tPunct && t.text == s }
func (p *parser) isWord(s string) bool {
	t := p.peek()
	return t.kind == tIdent && t.text == s
}
func (p *parser) eat(s string) bool {
	if p.is(s) {
		p.next()
		return true
	}
	return false
}

func (p *parser) fail(format string, args ...any) {
	if p.err == nil {
		p.err = fmt.Errorf(format, args...)
	}
}

func (p *parser) expect(s string) {
	if !p.eat(s) {
		p.fail("expected %s, found %q", s, p.peek().text)
	}
}

// text is the source between two token indexes.
func (p *parser) text(from, to int) string {
	if from >= len(p.toks) || to <= from {
		return ""
	}
	end := p.toks[to-1].off + len(p.toks[to-1].text)
	if p.toks[to-1].kind == tString {
		end = p.toks[to].off
		if to >= len(p.toks) {
			end = len(p.src)
		}
	}
	return strings.TrimSpace(p.src[p.toks[from].off:min(end, len(p.src))])
}

// matching is the index of the token closing the bracket at i, or -1.
func matching(toks []token, i int) int {
	open := toks[i].text
	close := map[string]string{"(": ")", "[": "]", "{": "}", "<": ">"}[open]
	depth := 0
	for j := i; j < len(toks); j++ {
		t := toks[j]
		if t.kind != tPunct {
			continue
		}
		switch {
		case t.text == open:
			depth++
		case t.text == close:
			depth--
			if depth == 0 {
				return j
			}
		case open == "<" && t.text == "=>":
		case open == "<" && (t.text == ";" || t.text == "{" && depth == 0):
			return -1
		}
	}
	return -1
}

// typ reads a type.
func (p *parser) typ() *Type {
	start := p.i
	p.eat("|")
	var members []*Type
	for {
		members = append(members, p.intersection())
		if p.err != nil || !p.eat("|") {
			break
		}
	}
	if len(members) == 1 {
		return members[0]
	}
	t := &Type{Kind: Union, Union: members}
	t.Text = p.text(start, p.i)
	return t
}

func (p *parser) intersection() *Type {
	start := p.i
	t := p.postfix()
	if !p.is("&") {
		return t
	}
	parts := []*Type{t}
	for p.eat("&") {
		parts = append(parts, p.postfix())
	}
	return &Type{Kind: Ref, Name: "&", Args: parts, Text: p.text(start, p.i)}
}

func (p *parser) postfix() *Type {
	start := p.i
	t := p.primary()
	for p.is("[") && !p.peek().nl && p.err == nil {
		if p.at(1).kind == tPunct && p.at(1).text == "]" {
			p.next()
			p.next()
			t = &Type{Kind: Array, Elem: t, Text: p.text(start, p.i)}
			continue
		}
		p.next()
		p.typ()
		p.expect("]")
		t = &Type{Kind: Unknown, Text: p.text(start, p.i)}
	}
	return t
}

func (p *parser) primary() *Type {
	start := p.i
	tk := p.peek()
	switch tk.kind {
	case tString:
		p.next()
		return &Type{Kind: StrLit, Lit: tk.text, Text: p.text(start, p.i)}
	case tNumber:
		p.next()
		return &Type{Kind: NumLit, Lit: tk.text, Text: tk.text}
	case tTemplate:
		p.next()
		return &Type{Kind: String, Text: tk.text}
	case tPunct:
		switch tk.text {
		case "(":
			end := matching(p.toks, p.i)
			if end > 0 && end+1 < len(p.toks) && p.toks[end+1].kind == tPunct && p.toks[end+1].text == "=>" {
				return p.funcType()
			}
			p.next()
			t := p.typ()
			p.expect(")")
			return t
		case "<": // a generic function type: <T>(x: T) => void
			p.fail("generic function types aren't supported")
			return &Type{}
		case "{":
			return p.object()
		case "[":
			p.next()
			var elems []*Type
			for !p.is("]") && p.err == nil && p.peek().kind != tEOF {
				p.eat("...")
				if p.peek().kind == tIdent && (p.at(1).text == ":" || p.at(1).text == "?" && p.at(2).text == ":") {
					p.next() // a named member: [day: string]
					p.eat("?")
					p.next()
				}
				elems = append(elems, p.typ())
				p.eat("?")
				if !p.eat(",") {
					break
				}
			}
			p.expect("]")
			return &Type{Kind: Tuple, Union: elems, Text: p.text(start, p.i)}
		case "-":
			p.next()
			if p.peek().kind == tNumber {
				n := p.next()
				return &Type{Kind: NumLit, Lit: "-" + n.text, Text: "-" + n.text}
			}
		}
		p.fail("unexpected %q in a type", tk.text)
		return &Type{}
	case tIdent:
		switch tk.text {
		case "string":
			p.next()
			return &Type{Kind: String, Text: "string"}
		case "number", "bigint":
			p.next()
			return &Type{Kind: Number, Text: tk.text}
		case "boolean":
			p.next()
			return &Type{Kind: Boolean, Text: "boolean"}
		case "true", "false":
			p.next()
			return &Type{Kind: BoolLit, Lit: tk.text, Text: tk.text}
		case "null":
			p.next()
			return &Type{Kind: Null, Text: "null"}
		case "undefined", "void":
			p.next()
			return &Type{Kind: Undefined, Text: tk.text}
		case "any", "unknown", "object":
			p.next()
			return &Type{Kind: Any, Text: tk.text}
		case "readonly":
			p.next()
			return p.postfix()
		case "typeof", "keyof", "infer", "unique":
			p.next()
			p.postfix()
			return &Type{Kind: Unknown, Text: p.text(start, p.i)}
		case "new", "abstract":
			p.next()
			if p.is("(") {
				p.funcType()
			}
			return &Type{Kind: Unknown, Text: p.text(start, p.i)}
		}
		name := p.next().text
		for p.is(".") && p.at(1).kind == tIdent {
			p.next()
			name += "." + p.next().text
		}
		t := &Type{Kind: Ref, Name: name}
		if p.is("<") && !p.peek().nl {
			p.next()
			for !p.is(">") && p.err == nil && p.peek().kind != tEOF {
				t.Args = append(t.Args, p.typ())
				if !p.eat(",") {
					break
				}
			}
			p.expect(">")
		}
		t.Text = p.text(start, p.i)
		return t
	}
	p.fail("unexpected end of a type")
	return &Type{}
}

// funcType reads (a: T, b?: U) => R.
func (p *parser) funcType() *Type {
	start := p.i
	t := &Type{Kind: Func}
	t.Params = p.params()
	p.expect("=>")
	p.typ() // the result: not part of the contract
	t.Text = p.text(start, p.i)
	return t
}

// params reads a parenthesized parameter list of a function type or a
// method signature.
func (p *parser) params() []*Param {
	p.expect("(")
	var out []*Param
	for !p.is(")") && p.err == nil && p.peek().kind != tEOF {
		pr := &Param{}
		pr.Rest = p.eat("...")
		switch {
		case p.peek().kind == tIdent:
			pr.Name = p.next().text
		case p.is("{") || p.is("["):
			end := matching(p.toks, p.i)
			if end < 0 {
				p.fail("unbalanced parameter")
				return out
			}
			pr.Name = "arg"
			p.i = end + 1
		default:
			p.fail("unexpected %q in parameters", p.peek().text)
			return out
		}
		pr.Optional = p.eat("?")
		if p.eat(":") {
			pr.Type = p.typ()
		} else {
			pr.Type = &Type{Kind: Any, Text: "any"}
		}
		if p.eat("=") { // a default in a signature (not legal TS, but harmless)
			p.skipExpr()
			pr.Optional = true
		}
		out = append(out, pr)
		if !p.eat(",") {
			break
		}
	}
	p.expect(")")
	return out
}

// object reads { members }.
func (p *parser) object() *Type {
	start := p.i
	p.expect("{")
	t := &Type{Kind: Object}
	for !p.is("}") && p.err == nil && p.peek().kind != tEOF {
		if p.eat(";") || p.eat(",") {
			continue
		}
		p.member(t)
	}
	p.expect("}")
	t.Text = p.text(start, p.i)
	// { [k: string]: T } alone is a record.
	if len(t.Fields) == 1 && t.Fields[0].Name == "[index]" {
		return &Type{Kind: Record, Elem: t.Fields[0].Type, Text: t.Text}
	}
	return t
}

// member reads one member of an object type into t.
func (p *parser) member(t *Type) {
	line := p.peek().off
	if p.isWord("readonly") && p.at(1).kind != tPunct || p.isWord("readonly") && p.at(1).text == "[" {
		p.next()
	}
	switch {
	case p.is("["): // an index signature
		p.next()
		if p.peek().kind != tIdent || p.at(1).text != ":" {
			p.fail("computed members aren't supported")
			return
		}
		p.next()
		p.next()
		key := p.typ()
		p.expect("]")
		p.expect(":")
		val := p.typ()
		if key.Kind != String {
			val = &Type{Kind: Unknown, Text: "[key: " + key.String() + "]: " + val.String()}
		}
		t.Fields = append(t.Fields, &Field{Name: "[index]", Type: val, Off: line})
		return
	case p.is("(") || p.is("<"): // a call signature
		ft := &Type{Kind: Func}
		s := p.i
		if p.is("<") {
			if end := matching(p.toks, p.i); end > 0 {
				p.i = end + 1
			}
		}
		ft.Params = p.params()
		if p.eat(":") {
			p.typ()
		}
		ft.Text = p.text(s, p.i)
		t.Fields = append(t.Fields, &Field{Name: "(call)", Type: ft, Off: line})
		return
	}
	var name string
	switch tk := p.peek(); tk.kind {
	case tIdent, tString, tNumber:
		name = p.next().text
	default:
		p.fail("unexpected %q in an object type", tk.text)
		return
	}
	f := &Field{Name: name, Off: line}
	f.Optional = p.eat("?")
	switch {
	case p.is("(") || p.is("<"): // a method signature
		ft := &Type{Kind: Func}
		s := p.i
		if p.is("<") {
			if end := matching(p.toks, p.i); end > 0 {
				p.i = end + 1
			}
		}
		ft.Params = p.params()
		if p.eat(":") {
			p.typ()
		}
		ft.Text = p.text(s, p.i)
		f.Type = ft
	case p.eat(":"):
		f.Type = p.typ()
	default:
		f.Type = &Type{Kind: Any, Text: "any"}
	}
	t.Fields = append(t.Fields, f)
}

// skipExpr skips an expression up to a , ) ] } or ; at its own depth.
func (p *parser) skipExpr() {
	depth := 0
	for p.peek().kind != tEOF {
		t := p.peek()
		if t.kind == tPunct {
			switch t.text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				if depth == 0 {
					return
				}
				depth--
			case ",", ";":
				if depth == 0 {
					return
				}
			}
		}
		p.next()
	}
}
