package transpile

import (
	"fmt"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// enumDecl is an enum declaration:
//
//	enum Status { Draft, Sent, Paid }                      a value enum, int
//	enum Role string { Admin = "admin", Staff = "staff" }  a value enum of another type
//	enum Shape { Circle{Radius float64}; Empty }           an enum with data
//
// It is rewritten in place into Go declarations, one variant a line as
// written, and its generated methods go into the trailer.
type enumDecl struct {
	name     string
	start    int // the enum keyword
	nameEnd  int
	base     string // the underlying type as written; "" for int
	baseEnd  int
	lbrace   int
	rbrace   int
	str      bool // a string enum
	sum      bool // variants hold data: a sealed interface and a struct each
	variants []*enumVariant
	commas   []int // offsets of the commas between variants
}

type enumVariant struct {
	name   string
	off    int
	value  span // after =; start < 0 when absent
	fields bool // written with {…}
}

// sealName is the name of the method sealing an enum with data: isShape.
func sealName(enum string) string { return "is" + upperFirst(enum) }

// parseName is the Go name of a value enum's Parse: ParseStatus, or
// parseStatus for an unexported status.
func parseName(enum string) string {
	if token.IsExported(enum) {
		return "Parse" + enum
	}
	return "parse" + upperFirst(enum)
}

func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

// enumAt reads the enum declaration whose keyword is toks[i]. It returns the
// index of its closing brace.
func (f *fileState) enumAt(toks []tok, i int) (*enumDecl, int, string) {
	d := &enumDecl{name: toks[i+1].lit, start: toks[i].off, nameEnd: toks[i+1].end()}
	j := i + 2
	if j < len(toks) && toks[j].tok == token.LBRACK {
		return nil, i, "an enum can't have type parameters"
	}
	if j < len(toks) && toks[j].tok == token.IDENT {
		d.base, d.baseEnd = toks[j].lit, toks[j].end()
		switch d.base {
		case "string":
			d.str = true
		case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "byte", "rune":
		default:
			return nil, i, "an enum's type is string or an integer type, not " + d.base
		}
		j++
	}
	if j >= len(toks) || toks[j].tok != token.LBRACE {
		return nil, i, "expected enum " + d.name + " { Variant, … }"
	}
	d.lbrace = toks[j].off
	end, _, ok := matchClose(toks, j)
	if !ok {
		return nil, i, "unclosed enum " + d.name
	}
	last := end - 1
	d.rbrace = toks[last].off
	seen := map[string]bool{}
	values := map[string]string{}
	withValue := 0
	k := j + 1
	for k < last {
		t := toks[k]
		if t.tok == token.COMMENT || t.tok == token.SEMICOLON {
			k++
			continue
		}
		if t.tok != token.IDENT {
			return nil, k, "expected a variant's name in enum " + d.name + ", found " + tokText(t)
		}
		if r, _ := utf8.DecodeRuneInString(t.lit); !unicode.IsUpper(r) {
			return nil, k, "a variant's name starts with a capital letter: " + upperFirst(t.lit)
		}
		if seen[t.lit] {
			return nil, k, d.name + "." + t.lit + " is declared twice"
		}
		seen[t.lit] = true
		v := &enumVariant{name: t.lit, off: t.off, value: span{-1, -1}}
		d.variants = append(d.variants, v)
		k++
		switch {
		case k < last && toks[k].tok == token.LBRACE:
			next, _, ok := matchClose(toks, k)
			if !ok || next > last {
				return nil, k, "unclosed fields of " + d.name + "." + v.name
			}
			v.fields, d.sum = true, true
			k = next
		case k < last && toks[k].tok == token.ASSIGN:
			k++
			from, depth := k, 0
			for ; k < last; k++ {
				tt := toks[k].tok
				if depth == 0 && (tt == token.COMMA || tt == token.SEMICOLON) {
					break
				}
				switch {
				case isOpen(tt):
					depth++
				case isClose(tt):
					depth--
				}
			}
			e := k - 1
			for e >= from && toks[e].tok == token.COMMENT {
				e--
			}
			if e < from {
				return nil, k, d.name + "." + v.name + " = needs a value"
			}
			v.value = span{toks[from].off, toks[e].end()}
			withValue++
			if e == from && (toks[from].tok == token.STRING || toks[from].tok == token.INT) {
				lit := toks[from].lit
				if other, dup := values[lit]; dup {
					return nil, from, d.name + "." + v.name + " has the value of " + d.name + "." + other
				}
				values[lit] = v.name
			}
		}
		for k < last && toks[k].tok == token.COMMENT {
			k++
		}
		switch {
		case k >= last:
		case toks[k].tok == token.COMMA:
			d.commas = append(d.commas, toks[k].off)
			k++
		case toks[k].tok == token.SEMICOLON:
			k++
		default:
			return nil, k, "expected , or a new line after " + d.name + "." + v.name
		}
	}
	switch {
	case len(d.variants) == 0:
		return nil, i, "enum " + d.name + " needs at least one variant"
	case d.sum && d.base != "":
		return nil, i, "an enum whose variants hold data has no underlying type: enum " + d.name + " { … }"
	case d.sum && withValue > 0:
		return nil, i, "an enum whose variants hold data can't give them values"
	case withValue > 0 && withValue < len(d.variants):
		return nil, i, "give every variant of " + d.name + " a value, or none"
	}
	if !d.sum {
		for _, v := range d.variants {
			if v.name == "Values" || v.name == "Parse" {
				return nil, i, d.name + "." + v.name + " is generated for every enum; name the variant differently"
			}
		}
	}
	return d, last, ""
}

func tokText(t tok) string {
	if t.lit != "" && t.tok != token.SEMICOLON {
		return t.lit
	}
	return t.tok.String()
}

// lowerEnum rewrites d in place and writes its methods for the trailer.
func (f *fileState) lowerEnum(toks []tok, d *enumDecl) {
	f.add(d.start, d.start+len("enum"), "type")
	for _, c := range d.commas {
		f.add(c, c+1, ";")
	}
	f.add(d.rbrace, d.rbrace+1, ")")
	w := &f.enumGen
	directive := func() {
		if !f.bare {
			p := f.at(d.start)
			w.gen("//line "+p.Filename+":"+itoa(p.Line)+":"+itoa(p.Column)+"\n", d.start)
		}
	}
	if d.sum {
		seal := sealName(d.name)
		f.add(d.nameEnd, d.lbrace+1, " interface{ "+seal+"() }; type (")
		for _, v := range d.variants {
			text := d.name + v.name + " struct"
			if !v.fields {
				text += "{}"
			}
			f.add(v.off, v.off+len(v.name), text)
		}
		w.gen("\n", d.start)
		directive()
		for _, v := range d.variants {
			w.gen("func ("+d.name+v.name+") "+seal+"() {}\n", d.start)
		}
		return
	}
	if d.base == "" {
		f.add(d.nameEnd, d.lbrace+1, " int; const (")
	} else {
		f.add(d.baseEnd, d.lbrace+1, "; const (")
	}
	for i, v := range d.variants {
		text := d.name + v.name
		switch {
		case v.value.start >= 0:
			text += " " + d.name
		case d.str:
			text += " " + d.name + " = " + strconv.Quote(v.name)
		case i == 0:
			text += " " + d.name + " = iota"
		}
		f.add(v.off, v.off+len(v.name), text)
	}
	rt := f.scannedRuntime(toks)
	fmtName := f.scannedImport(toks, "fmt")
	driverName := f.scannedImport(toks, "database/sql/driver")
	w.gen("\n", d.start)
	directive()
	w.gen(valueEnumMethods(d, rt, fmtName, driverName), d.start)
}

// valueEnumMethods is the Go generated for a value enum: Values, Parse,
// String, Valid, MarshalText, UnmarshalText, Scan and Value.
func valueEnumMethods(d *enumDecl, rt, fmtName, driverName string) string {
	T := d.name
	r := strings.ToLower(T[:1])
	if !unicode.IsLetter(rune(r[0])) {
		r = "e"
	}
	verb := "%d"
	if d.str {
		verb = "%q"
	}
	var names []string
	for _, v := range d.variants {
		names = append(names, T+v.name)
	}
	var b strings.Builder
	p := func(format string, args ...any) {
		fmt.Fprintf(&b, format, args...)
	}
	values, parse := T+"Values", parseName(T)

	p("// %s returns every %s, in the order they are declared.\n", values, T)
	p("func %s() []%s {\n\treturn []%s{%s}\n}\n\n", values, T, T, strings.Join(names, ", "))

	if d.str {
		p("// %s reads %s from its value or its name.\n", parse, article(T))
	} else {
		p("// %s reads %s from its name.\n", parse, article(T))
	}
	p("func %s(text string) %s.Result[%s] {\n\tswitch text {\n", parse, rt, T)
	if d.str {
		for _, v := range d.variants {
			p("\tcase string(%s%s):\n\t\treturn %s.Ok(%s%s)\n", T, v.name, rt, T, v.name)
		}
		var named []*enumVariant
		for _, v := range d.variants {
			if v.value.start < 0 { // its value is its name
				continue
			}
			named = append(named, v)
		}
		if len(named) > 0 {
			p("\t}\n\tswitch text {\n")
			for _, v := range named {
				p("\tcase %q:\n\t\treturn %s.Ok(%s%s)\n", v.name, rt, T, v.name)
			}
		}
	} else {
		for _, v := range d.variants {
			p("\tcase %q:\n\t\treturn %s.Ok(%s%s)\n", v.name, rt, T, v.name)
		}
	}
	p("\t}\n\treturn %s.Err[%s](%s.Errorf(\"invalid %s %%q\", text))\n}\n\n", rt, T, fmtName, T)

	if d.str {
		p("// String returns the value of %s.\n", r)
		p("func (%s %s) String() string { return string(%s) }\n\n", r, T, r)
	} else {
		p("// String returns the name of %s.\n", r)
		p("func (%s %s) String() string {\n\tswitch %s {\n", r, T, r)
		for _, v := range d.variants {
			p("\tcase %s%s:\n\t\treturn %q\n", T, v.name, v.name)
		}
		p("\t}\n\treturn %s.Sprintf(\"%s(%%d)\", %s)\n}\n\n", fmtName, T, r)
	}

	p("// Valid reports whether %s is one of the variants of %s.\n", r, T)
	p("func (%s %s) Valid() bool {\n\tswitch %s {\n\tcase %s:\n\t\treturn true\n\t}\n\treturn false\n}\n\n", r, T, r, strings.Join(names, ", "))

	form := "name"
	if d.str {
		form = "value"
	}
	p("// MarshalText writes %s as its %s, as JSON and other text encodings show it.\n", r, form)
	p("func (%s %s) MarshalText() ([]byte, error) {\n\tif !%s.Valid() {\n\t\treturn nil, %s.Errorf(\"invalid %s %s\", %s)\n\t}\n\treturn []byte(%s.String()), nil\n}\n\n",
		r, T, r, fmtName, T, verb, unconv(d, r), r)

	p("// UnmarshalText reads %s as %s does.\n", article(T), parse)
	p("func (%s *%s) UnmarshalText(text []byte) error {\n\tval, err := %s(string(text)).Get()\n\tif err != nil {\n\t\treturn err\n\t}\n\t*%s = val\n\treturn nil\n}\n\n",
		r, T, parse, r)

	if d.str {
		p("// Scan reads %s from a database column holding its value or its name.\n", article(T))
	} else {
		p("// Scan reads %s from a database column holding its number or its name.\n", article(T))
	}
	p("func (%s *%s) Scan(src any) error {\n\tswitch src := src.(type) {\n", r, T)
	if !d.str {
		p("\tcase int64:\n\t\tif val := %s(src); val.Valid() {\n\t\t\t*%s = val\n\t\t\treturn nil\n\t\t}\n\t\treturn %s.Errorf(\"invalid %s %%d\", src)\n", T, r, fmtName, T)
	}
	p("\tcase string:\n\t\treturn %s.UnmarshalText([]byte(src))\n\tcase []byte:\n\t\treturn %s.UnmarshalText(src)\n\t}\n", r, r)
	p("\treturn %s.Errorf(\"can't scan a %%T into a %s\", src)\n}\n\n", fmtName, T)

	store, conv := "number", "int64("+r+")"
	if d.str {
		store, conv = "value", "string("+r+")"
	}
	p("// Value stores %s in a database column as its %s.\n", r, store)
	p("func (%s %s) Value() (%s.Value, error) {\n\tif !%s.Valid() {\n\t\treturn nil, %s.Errorf(\"invalid %s %s\", %s)\n\t}\n\treturn %s, nil\n}\n",
		r, T, driverName, r, fmtName, T, verb, unconv(d, r), conv)
	return b.String()
}

// unconv is r as fmt prints it without calling String: its number, or its
// string.
func unconv(d *enumDecl, r string) string {
	if d.str {
		return "string(" + r + ")"
	}
	return r
}

// scannedImport is the name f imports path as, read from the tokens before
// the file can be parsed; the import is added when f lacks it.
func (f *fileState) scannedImport(toks []tok, path string) string {
	if f.scannedImports == nil {
		f.scannedImports = map[string]string{}
	}
	if name, ok := f.scannedImports[path]; ok {
		return name
	}
	quoted := strconv.Quote(path)
	pkgEnd, inImports := -1, false
	name := ""
scan:
	for k, t := range toks {
		switch {
		case t.tok == token.PACKAGE && k+1 < len(toks):
			pkgEnd = toks[k+1].end()
		case t.tok == token.IMPORT:
			inImports = true
		case t.tok == token.FUNC || t.tok == token.TYPE || t.tok == token.VAR || t.tok == token.CONST || isAt(t) || t.tok == token.IDENT && t.lit == "enum" && k > 0 && toks[k-1].tok == token.SEMICOLON:
			break scan
		case inImports && t.tok == token.STRING && t.lit == quoted:
			alias := ""
			if k > 0 && toks[k-1].tok == token.IDENT {
				alias = toks[k-1].lit
			} else if k > 0 && toks[k-1].tok == token.PERIOD {
				alias = "."
			}
			switch alias {
			case "":
				name = ImportName(path)
			case ".", "_":
				continue
			default:
				name = alias
			}
			break scan
		}
	}
	if name == "" {
		name = ImportName(path)
		if pkgEnd >= 0 { // else no package clause: the parse reports it
			f.insert(pkgEnd, "; import "+quoted, 0)
		}
	}
	f.scannedImports[path] = name
	return name
}

// enumInfo is an enum as its Go declares it, whichever package it is in: a
// value enum is a defined integer or string type T with a func TValues() []T,
// its variants T's constants named TVariant; an enum with data is an
// interface T whose one method is isT(), its variants the package's types
// implementing it.
type enumInfo struct {
	named    *types.Named
	sum      bool
	variants []enumMember
}

type enumMember struct {
	name string       // as Vuka writes it after the dot: Draft
	obj  types.Object // *types.Const, or *types.TypeName for an enum with data
}

// enumOf is t's enum, or nil when t isn't one.
func enumOf(t types.Type) *enumInfo {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.TypeArgs().Len() > 0 || n.TypeParams().Len() > 0 || n.Obj().Pkg() == nil {
		return nil
	}
	name, scope := n.Obj().Name(), n.Obj().Pkg().Scope()
	info := &enumInfo{named: n}
	switch u := n.Underlying().(type) {
	case *types.Interface:
		if u.NumExplicitMethods() != 1 || u.NumEmbeddeds() != 0 {
			return nil
		}
		m := u.ExplicitMethod(0)
		if sig := m.Type().(*types.Signature); m.Name() != sealName(name) || sig.Params().Len() != 0 || sig.Results().Len() != 0 {
			return nil
		}
		info.sum = true
		for _, s := range scope.Names() {
			tn, ok := scope.Lookup(s).(*types.TypeName)
			if !ok || tn.IsAlias() || tn.Type() == n || types.IsInterface(tn.Type()) {
				continue
			}
			if vn, ok := tn.Type().(*types.Named); !ok || vn.TypeParams().Len() > 0 || !types.Implements(vn, u) {
				continue
			}
			info.variants = append(info.variants, enumMember{variantName(name, s), tn})
		}
	case *types.Basic:
		if u.Info()&(types.IsInteger|types.IsString) == 0 {
			return nil
		}
		fn, ok := scope.Lookup(name + "Values").(*types.Func)
		if !ok {
			return nil
		}
		sig := fn.Type().(*types.Signature)
		if sig.Params().Len() != 0 || sig.Results().Len() != 1 {
			return nil
		}
		if sl, ok := sig.Results().At(0).Type().(*types.Slice); !ok || !types.Identical(sl.Elem(), n) {
			return nil
		}
		for _, s := range scope.Names() {
			c, ok := scope.Lookup(s).(*types.Const)
			if !ok || !types.Identical(c.Type(), n) || !strings.HasPrefix(s, name) || len(s) == len(name) {
				continue
			}
			info.variants = append(info.variants, enumMember{s[len(name):], c})
		}
	default:
		return nil
	}
	if len(info.variants) == 0 {
		return nil
	}
	sort.SliceStable(info.variants, func(i, j int) bool { return info.variants[i].obj.Pos() < info.variants[j].obj.Pos() })
	return info
}

func variantName(enum, goName string) string {
	if v, ok := strings.CutPrefix(goName, enum); ok && v != "" {
		return v
	}
	return goName
}

// variant is the variant obj declares, or nil.
func (info *enumInfo) variant(obj types.Object) *enumMember {
	for i := range info.variants {
		if info.variants[i].obj == obj {
			return &info.variants[i]
		}
	}
	return nil
}

// byName is the variant written name, or nil.
func (info *enumInfo) byName(name string) *enumMember {
	for i := range info.variants {
		if info.variants[i].name == name {
			return &info.variants[i]
		}
	}
	return nil
}

// enumStatic is what Enum.member names on an enum: a variant, or a value
// enum's Values or Parse.
func enumStatic(t *types.Named, member string) types.Object {
	info := enumOf(t)
	if info == nil {
		return nil
	}
	if v := info.byName(member); v != nil {
		return v.obj
	}
	if info.sum {
		return nil
	}
	name, scope := t.Obj().Name(), t.Obj().Pkg().Scope()
	switch member {
	case "Values":
		return scope.Lookup(name + "Values")
	case "Parse":
		return scope.Lookup(parseName(name))
	}
	return nil
}

// ctorKey is the exhaustiveness constructor of an enum's variant.
func ctorKey(obj types.Object) string { return "enum:" + obj.Pkg().Path() + "." + obj.Name() }

// article is name after the article its first letter takes: an Employer,
// a Status.
func article(name string) string {
	if name != "" && strings.ContainsRune("AEIOU", unicode.ToUpper(rune(name[0]))) {
		return "an " + name
	}
	return "a " + name
}
