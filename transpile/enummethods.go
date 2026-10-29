package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

// Methods on a value enum's variants. A value enum's variants are constants
// of one named type, so Go has no type for a variant to hold a method:
//
//	func (PasswordError.Mismatch) Error() string { return "Passwords do not match" }
//
// becomes a function holding the body, passwordErrorMismatchError, and each
// method name becomes one method on the enum switching on the value:
//
//	func (p PasswordError) Error() string {
//		switch p {
//		case PasswordErrorMismatch:
//			return passwordErrorMismatchError()
//		…
//		}
//		panic(…) // or the default: the method declared on the enum itself
//	}
//
// Every variant declares the method, or the enum does (the default). Each
// declaration has the method's one signature, a value receiver, and a name
// the enum doesn't generate (String, Valid, …).

// variantMethod is one declaration of a per-variant method: a variant's, or
// the default on the enum.
type variantMethod struct {
	file    *fileState
	decl    *ast.FuncDecl
	variant string // "" for the default
	recv    string // the receiver's name; "" when unnamed or _
	helper  string // the function holding its body
	sig     string // its parameters and results, as compared
}

// variantMethodSet is a method name declared on a value enum's variants.
type variantMethodSet struct {
	enum    *enumDecl
	name    string
	list    []*variantMethod // variants', in source order
	def     *variantMethod   // the default, or nil
	defs    []*variantMethod // every declaration on the enum of the name
	file    *fileState       // where the dispatching method is written
	nameOff int              // src offset of the first variant's method name
}

// VariantMethod is a value enum's variant method as the source declares it,
// for the language server: Header spans the declaration from func to the
// end of its results, Name its name.
type VariantMethod struct {
	Enum, Variant, Method string // Variant "" for the default on the enum
	Header, Name          [2]int // src offsets
	// Helper is the Go function holding its body.
	Helper string
	// Dispatch is the src offset the dispatching method is attributed to:
	// the name of the set's first variant method.
	Dispatch int
}

// enumGenerated are the methods every value enum generates.
var enumGenerated = map[string]bool{"String": true, "Valid": true, "MarshalText": true, "UnmarshalText": true, "Scan": true, "Value": true}

// lowerVariantMethods lowers the package's methods on value-enum variants.
// It reports whether it rewrote anything.
func (e *engine) lowerVariantMethods() bool {
	enums := map[string]*enumDecl{}
	enumFile := map[string]*fileState{}
	for _, f := range e.vuka {
		for _, d := range f.enums {
			if !d.sum {
				enums[d.name], enumFile[d.name] = d, f
			}
		}
	}
	if len(enums) == 0 {
		return false
	}
	sets := map[string]*variantMethodSet{}
	var order []string
	setOf := func(d *enumDecl, name string) *variantMethodSet {
		key := d.name + "." + name
		s := sets[key]
		if s == nil {
			s = &variantMethodSet{enum: d, name: name}
			sets[key] = s
			order = append(order, key)
		}
		return s
	}
	nerrs := len(*e.errs)
	for _, f := range e.vuka {
		for _, decl := range f.ast.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || len(fd.Recv.List) != 1 || fd.Name.Name == "_" {
				continue
			}
			field := fd.Recv.List[0]
			t, ptr := field.Type, false
			for {
				if p, ok := t.(*ast.ParenExpr); ok {
					t = p.X
					continue
				}
				if s, ok := t.(*ast.StarExpr); ok && !ptr {
					t, ptr = s.X, true
					continue
				}
				break
			}
			m := &variantMethod{file: f, decl: fd}
			if len(field.Names) == 1 && field.Names[0].Name != "_" {
				m.recv = field.Names[0].Name
			}
			var d *enumDecl
			switch x := t.(type) {
			case *ast.SelectorExpr:
				id, ok := x.X.(*ast.Ident)
				if !ok || enums[id.Name] == nil {
					continue
				}
				d, m.variant = enums[id.Name], x.Sel.Name
				known := false
				var all []string
				for _, v := range d.variants {
					known = known || v.name == m.variant
					all = append(all, v.name)
				}
				if !known {
					e.errs.add("E0164", f.nodePos(x.Sel.Pos()), "%s has no variant %s", d.name, m.variant).
						help("%s's variants are %s", d.name, strings.Join(all, ", "))
					continue
				}
				if ptr {
					e.errs.add("E0166", f.nodePos(field.Type.Pos()), "%s.%s.%s has a pointer receiver: %s is a value enum, its variants are constants; write the receiver %s.%s",
						d.name, m.variant, fd.Name.Name, d.name, d.name, m.variant)
					// It still covers its variant: no "has no method for" too.
				}
				if enumGenerated[fd.Name.Name] {
					e.errs.add("E0167", f.nodePos(fd.Name.Pos()), "%s.%s can't declare %s: every value enum generates %s.%s",
						d.name, m.variant, fd.Name.Name, d.name, fd.Name.Name).
						help("remove it; %s", generatedNote(d, fd.Name.Name))
					continue
				}
			case *ast.Ident:
				if enums[x.Name] == nil {
					continue
				}
				d = enums[x.Name]
				if ptr {
					m.variant = "*" // a pointer method on the enum: a default only when the name is per-variant
				}
			default:
				continue
			}
			m.sig = methodSig(fd)
			s := setOf(d, fd.Name.Name)
			if m.variant == "" || m.variant == "*" {
				s.defs = append(s.defs, m)
				continue
			}
			s.list = append(s.list, m)
		}
	}
	var lowered []*variantMethodSet
	for _, key := range order {
		s := sets[key]
		if len(s.list) == 0 {
			continue // a plain method of the enum
		}
		d := s.enum
		first := s.list[0]
		seen := map[string]*variantMethod{}
		for _, m := range s.list {
			if prev := seen[m.variant]; prev != nil {
				e.errs.add("E0165", m.file.nodePos(m.decl.Name.Pos()), "%s.%s.%s is declared twice: here and at %s",
					d.name, m.variant, s.name, prev.file.nodePos(prev.decl.Name.Pos())).Related = []token.Position{prev.file.nodePos(prev.decl.Name.Pos())}
				continue
			}
			seen[m.variant] = m
			if m.sig != first.sig {
				e.errs.add("E0165", m.file.nodePos(m.decl.Name.Pos()), "%s.%s.%s is %s, but %s.%s.%s at %s is %s: every variant's %s has one signature",
					d.name, m.variant, s.name, sigShow(s.name, m.sig), d.name, first.variant, s.name, first.file.nodePos(first.decl.Name.Pos()), sigShow(s.name, first.sig), s.name).
					Related = []token.Position{first.file.nodePos(first.decl.Name.Pos())}
			}
		}
		for i, m := range s.defs {
			pos := m.file.nodePos(m.decl.Name.Pos())
			switch {
			case i > 0:
				prev := s.defs[0]
				e.errs.add("E0165", pos, "%s.%s is declared twice: here and at %s", d.name, s.name, prev.file.nodePos(prev.decl.Name.Pos())).
					Related = []token.Position{prev.file.nodePos(prev.decl.Name.Pos())}
			case m.variant == "*":
				e.errs.add("E0166", m.file.nodePos(m.decl.Recv.List[0].Type.Pos()), "%s.%s is declared on %s's variants, so its default on %s takes a value receiver, not *%s",
					d.name, s.name, d.name, d.name, d.name)
			case m.sig != first.sig:
				e.errs.add("E0165", pos, "%s.%s is %s, but %s.%s.%s at %s is %s: the default on %s has the variants' signature",
					d.name, s.name, sigShow(s.name, m.sig), d.name, first.variant, s.name, first.file.nodePos(first.decl.Name.Pos()), sigShow(s.name, first.sig), d.name).
					Related = []token.Position{first.file.nodePos(first.decl.Name.Pos())}
			default:
				s.def = m
			}
		}
		if len(s.defs) == 0 {
			var miss []string
			for _, v := range d.variants {
				if seen[v.name] == nil {
					miss = append(miss, v.name)
				}
			}
			if len(miss) > 0 {
				e.errs.add("E0164", first.file.nodePos(first.decl.Name.Pos()), "%s.%s has no method for %s: add them, or a default on %s",
					d.name, s.name, strings.Join(miss, ", "), d.name).
					help("declare func (%s %s) %s for the variants without their own", lowerFirst(d.name)[:1], d.name, sigShow(s.name, first.sig))
			}
		}
		s.file, s.nameOff = first.file, first.file.orig(first.decl.Name.Pos())
		lowered = append(lowered, s)
	}
	if len(*e.errs) > nerrs || len(lowered) == 0 {
		return false
	}
	taken := declaredNames(e.files)
	name := func(base string) string {
		n := base
		for i := 2; taken["."+n]; i++ {
			n = base + itoa(i)
		}
		taken["."+n] = true
		return n
	}
	written := map[*fileState]bool{}
	for _, s := range lowered {
		d := s.enum
		for _, m := range s.list {
			m.helper = name(lowerFirst(d.name) + m.variant + upperFirst(s.name))
			e.lowerVariantDecl(m, d, s)
		}
		if s.def != nil {
			s.def.helper = name("default" + upperFirst(d.name) + upperFirst(s.name))
			e.lowerVariantDecl(s.def, d, s)
		}
		f := s.file
		w := &f.enumGen
		w.gen("\n", s.nameOff)
		if !f.bare {
			p := f.at(s.nameOff)
			w.gen("//line "+p.Filename+":"+itoa(p.Line)+":"+itoa(p.Column)+"\n", s.nameOff)
		}
		// The method's name maps to the first variant's: definition from
		// a call lands on a declaration of it.
		text := dispatchMethod(s)
		at := strings.Index(text, ") "+s.name+"(") + 2
		w.gen(text[:at], s.nameOff)
		w.replace(s.name, s.nameOff, len(s.name))
		w.gen(text[at+len(s.name):], s.nameOff+len(s.name))
		written[f] = true
	}
	for _, f := range e.vuka {
		if written[f] {
			f.makeTrailer(e.bare)
		}
	}
	return true
}

// lowerVariantDecl rewrites m's declaration into a function of its own:
// func (e PasswordError.Mismatch) Error() string is func
// passwordErrorMismatchError(e PasswordError) string.
func (e *engine) lowerVariantDecl(m *variantMethod, d *enumDecl, s *variantMethodSet) {
	f, fd := m.file, m.decl
	from := f.orig(fd.Recv.Opening)
	to := f.orig(fd.Type.Params.Opening) + 1
	text := m.helper + "("
	if m.recv != "" {
		text += m.recv + " " + d.name
		if fd.Type.Params.NumFields() > 0 {
			text += ", "
		}
	}
	f.add(from, to, text)
	nameOff := f.orig(fd.Name.Pos())
	f.renameDoc(nameOff, m.helper, s.name)
	f.variantMethods = append(f.variantMethods, VariantMethod{
		Enum: d.name, Variant: m.variant, Method: s.name,
		Header: [2]int{f.orig(fd.Pos()), f.orig(fd.Type.End())},
		Name:   [2]int{nameOff, nameOff + len(s.name)},
		Helper: m.helper, Dispatch: s.nameOff,
	})
}

// dispatchMethod is the method on s's enum calling each variant's.
func dispatchMethod(s *variantMethodSet) string {
	d := s.enum
	T := d.name
	r := strings.ToLower(T[:1])
	if r[0] < 'a' || r[0] > 'z' {
		r = "e"
	}
	proto := s.list[0]
	fd := proto.decl
	// The parameters keep their names when every one has a usable name.
	var names, types_ []string
	variadic := false
	named := true
	for _, field := range fd.Type.Params.List {
		typ := proto.file.nodeText(field.Type)
		if _, ok := field.Type.(*ast.Ellipsis); ok {
			variadic = true
		}
		if len(field.Names) == 0 {
			named = false
			names, types_ = append(names, ""), append(types_, typ)
			continue
		}
		for _, n := range field.Names {
			if n.Name == "_" || n.Name == r {
				named = false
			}
			names, types_ = append(names, n.Name), append(types_, typ)
		}
	}
	if !named {
		for i := range names {
			names[i] = "a" + itoa(i+1)
		}
	}
	var params []string
	for i := range names {
		params = append(params, names[i]+" "+types_[i])
	}
	results := ""
	if fd.Type.Results != nil {
		results = " " + proto.file.nodeText(fd.Type.Results)
	}
	hasResults := fd.Type.Results.NumFields() > 0
	call := func(m *variantMethod) string {
		var args []string
		if m.recv != "" {
			args = append(args, r)
		}
		for i, n := range names {
			if variadic && i == len(names)-1 {
				n += "..."
			}
			args = append(args, n)
		}
		return m.helper + "(" + strings.Join(args, ", ") + ")"
	}
	byVariant := map[string]*variantMethod{}
	for _, m := range s.list {
		byVariant[m.variant] = m
	}
	var b strings.Builder
	var covered []string
	for _, v := range d.variants {
		if byVariant[v.name] != nil {
			covered = append(covered, T+"."+v.name)
		}
	}
	b.WriteString("// " + s.name + " calls the " + s.name + " of " + r + "'s variant: " + strings.Join(covered, ", "))
	if s.def != nil {
		b.WriteString(", and the default on " + T + " for the others.\n")
	} else {
		b.WriteString(".\n")
	}
	b.WriteString("func (" + r + " " + T + ") " + s.name + "(" + strings.Join(params, ", ") + ")" + results + " {\n\tswitch " + r + " {\n")
	for _, v := range d.variants {
		m := byVariant[v.name]
		if m == nil {
			continue
		}
		b.WriteString("\tcase " + T + v.name + ":\n")
		if hasResults {
			b.WriteString("\t\treturn " + call(m) + "\n")
		} else {
			b.WriteString("\t\t" + call(m) + "\n\t\treturn\n")
		}
	}
	b.WriteString("\t}\n")
	switch {
	case s.def != nil && hasResults:
		b.WriteString("\treturn " + call(s.def) + "\n")
	case s.def != nil:
		b.WriteString("\t" + call(s.def) + "\n")
	case d.str:
		b.WriteString("\tpanic(\"vuka: " + T + "." + s.name + " called on an invalid " + T + ": \\\"\" + string(" + r + ") + \"\\\"\")\n")
	default:
		b.WriteString("\tpanic(\"vuka: " + T + "." + s.name + " called on an invalid " + T + ": \" + " + r + ".String())\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// methodSig is fd's parameters' and results' types, as compared across a
// method's declarations.
func methodSig(fd *ast.FuncDecl) string {
	list := func(fl *ast.FieldList) string {
		if fl == nil {
			return ""
		}
		var ts []string
		for _, field := range fl.List {
			for range max(1, len(field.Names)) {
				ts = append(ts, types.ExprString(field.Type))
			}
		}
		return strings.Join(ts, ", ")
	}
	return "(" + list(fd.Type.Params) + ")" + "|" + list(fd.Type.Results)
}

// sigShow is sig as Go writes a method of that name: Error() string.
func sigShow(name, sig string) string {
	params, results, _ := strings.Cut(sig, "|")
	switch {
	case results == "":
		return name + params
	case strings.Contains(results, ", "):
		return name + params + " (" + results + ")"
	}
	return name + params + " " + results
}

// generatedNote points at what a value enum generates under name.
func generatedNote(d *enumDecl, name string) string {
	switch name {
	case "String":
		if d.str {
			return d.name + ".String returns the variant's value"
		}
		return d.name + ".String returns the variant's name; for another text, declare a method of another name (Label, Message)"
	case "Valid":
		return d.name + ".Valid reports whether a value is one of the variants"
	case "Value", "Scan":
		return d.name + "." + name + " stores the enum in a database column"
	}
	return d.name + "." + name + " encodes the enum as text"
}

// variantMethodsOf is f's variant methods, in source order.
func (f *fileState) variantMethodsOf() []VariantMethod {
	out := append([]VariantMethod(nil), f.variantMethods...)
	sort.Slice(out, func(i, j int) bool { return out[i].Header[0] < out[j].Header[0] })
	return out
}
