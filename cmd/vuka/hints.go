package main

import (
	"go/ast"
	"go/build"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// What vuka adds to a Go error whose cause it is certain of: the signature
// a call doesn't fit, the request's ctx a template call leaves out, what
// generates a name that is missing, the import an attribute or a package
// name needs. The build's report and the editor's diagnostics both take
// them from goHints.

// hintInput is a Go error and where it is.
type hintInput struct {
	msg      string // Go's message, its lines after the first indented
	file     string // the source file
	src      []byte // its text; nil when unknown
	from, to int    // the error's span in src
	markup   bool   // in a template's markup, where ctx is the request's context
	names    map[string]string
	respell  func(string) string // the message as the source spells the code it quotes
	modRoot  string
	modPath  string
	types    *typeFacts // nil in the editor, which has only gopls's message
}

// typeFacts is what type-checking the package found around the error.
type typeFacts struct {
	info  *types.Info
	pkg   *types.Package
	pos   token.Pos
	call  *ast.CallExpr // the call an argument count error is about
	path  []ast.Node    // the nodes enclosing pos
	files []*checkedFile
}

// hint is a Go error as vuka reports it.
type hint struct {
	msg   string // the message
	label string // what the span shows
	help  string // what to do
	note  string // why
	// span, when set (to > from), is the source the error is about, as
	// byte offsets: the @ of an attribute, a package name without its
	// selector.
	span [2]int
}

func goHints(in hintInput) hint {
	msg := in.msg
	if in.respell != nil {
		msg = in.respell(msg)
	}
	msg = demangle(msg, in.names)
	h := hint{msg: msg}
	first, rest, _ := strings.Cut(msg, "\n")
	switch {
	case strings.HasPrefix(first, "not enough arguments in call to "), strings.HasPrefix(first, "too many arguments in call to "):
		in.arity(&h, first, rest)
	case strings.HasPrefix(first, "undefined: "):
		in.undefined(&h, strings.TrimPrefix(first, "undefined: "))
	case unusedImport.MatchString(first):
		h.help = "delete the import, or use the package"
	case strings.Contains(first, " has no field or method "):
		in.noField(&h, first)
	}
	return h
}

var unusedImport = regexp.MustCompile(`^"[^"]+" imported (as \w+ )?and not used$`)

// param is a parameter of a signature: its name ("" when unknown) and type.
type param struct{ name, typ string }

func (p param) String() string {
	if p.name == "" || p.name == "_" {
		return p.typ
	}
	return p.name + " " + p.typ
}

// arity explains a call with too few or too many arguments: what the
// function takes, and in a template, the request's ctx when that is what is
// left out.
func (in hintInput) arity(h *hint, first, rest string) {
	callee := first[strings.Index(first, " call to ")+len(" call to "):]
	short := callee[strings.LastIndexByte(callee, '.')+1:]
	var want []param
	var have []string
	fits := false // the arguments given fit the parameters after the first
	if t := in.types; t != nil && t.call != nil {
		if sig, ok := t.info.TypeOf(t.call.Fun).(*types.Signature); ok {
			qual := types.RelativeTo(t.pkg)
			for v := range sig.Params().Variables() {
				want = append(want, param{v.Name(), types.TypeString(v.Type(), qual)})
			}
			if sig.Variadic() && len(want) > 0 {
				want[len(want)-1].typ = "..." + strings.TrimPrefix(want[len(want)-1].typ, "[]")
			}
			fits = !sig.Variadic() && len(t.call.Args) == sig.Params().Len()-1 && t.call.Ellipsis == token.NoPos
			for i, a := range t.call.Args {
				at := t.info.TypeOf(a)
				if !fits || at == nil || !types.AssignableTo(at, sig.Params().At(i+1).Type()) {
					fits = false
					break
				}
			}
		}
	} else {
		for _, l := range strings.Split(rest, "\n") {
			l = strings.TrimSpace(l)
			if w, ok := strings.CutPrefix(l, "want "); ok {
				for _, t := range splitTypes(w) {
					want = append(want, param{typ: t})
				}
			} else if hv, ok := strings.CutPrefix(l, "have "); ok {
				have = splitTypes(hv)
			}
		}
		fits = len(want) > 0 && len(have) == len(want)-1
		for i := range have {
			fits = fits && have[i] == want[i+1].typ
		}
	}
	if want == nil {
		return
	}
	ps := make([]string, len(want))
	for i, p := range want {
		ps[i] = p.String()
	}
	h.msg, h.label = first, short+" takes ("+strings.Join(ps, ", ")+")"
	if !strings.HasPrefix(first, "not enough") || !fits || want[0].typ != "context.Context" || !in.markup && !in.ctxInScope() {
		return
	}
	what := "pass ctx"
	if in.markup {
		what = "pass the request's context"
	}
	if call := in.text(); strings.HasSuffix(call, ")") && strings.Contains(call, "(") {
		open := strings.Index(call, "(")
		arg := "ctx"
		if strings.TrimSpace(call[open+1:len(call)-1]) != "" {
			arg = "ctx, "
		}
		h.help = what + ": " + call[:open+1] + arg + call[open+1:]
		return
	}
	h.help = what + " as its first argument (ctx)"
}

// ctxInScope reports whether a ctx of type context.Context is in scope at
// the error, in Go code.
func (in hintInput) ctxInScope() bool {
	t := in.types
	if t == nil || t.pkg == nil {
		return false
	}
	s := t.pkg.Scope().Innermost(t.pos)
	if s == nil {
		return false
	}
	_, obj := s.LookupParent("ctx", t.pos)
	v, ok := obj.(*types.Var)
	return ok && types.TypeString(v.Type(), nil) == "context.Context"
}

// splitTypes splits a "(T1, T2)" of have/want into its types.
func splitTypes(s string) []string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "(") || !strings.HasSuffix(s, ")") {
		return nil
	}
	s = s[1 : len(s)-1]
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if t := strings.TrimSpace(s[start:]); t != "" {
		out = append(out, t)
	}
	return out
}

// text is the source the error spans.
func (in hintInput) text() string {
	if in.src == nil || in.from < 0 || in.to > len(in.src) || in.to <= in.from {
		return ""
	}
	return string(in.src[in.from:in.to])
}

// undefined explains a name that isn't declared: an attribute or package
// used without its import, a name vuka or the orm generates.
func (in hintInput) undefined(h *hint, name string) {
	qual, member, qualified := strings.Cut(name, ".")
	if !qualified {
		if attr, at := in.attributeAt(); attr != "" && strings.HasPrefix(attr, "@"+name+".") {
			h.label, h.span = name+" isn't imported", [2]int{at, at + len(attr)}
			if p := in.importFor(name); p != "" {
				h.help = "import " + strconv.Quote(p) + " for " + attr
			} else {
				h.help = "import the package that declares " + attr
			}
			return
		}
		if end := in.identEnd(); end < len(in.src) && in.src[end] == '.' && string(in.src[in.from:end]) == name {
			if p := in.importFor(name); p != "" {
				h.label, h.span = name+" isn't imported", [2]int{in.from, end}
				h.help = "import " + strconv.Quote(p)
				return
			}
		}
		member, qual = name, ""
	}
	if model, ok := strings.CutSuffix(member, "Fields"); ok && model != "" {
		in.typedFields(h, qual, member, model)
		return
	}
	if qual != "" {
		if dir := in.importedDir(qual); dir != "" && in.urlsDir(dir) {
			h.note = "the urls package is generated by vuka from the folders under pages/, and no page or folder is " + member + " (vuka routes lists them)"
		}
	}
}

// attributeAt is the @attribute the error is at ("@graphql.Query"), "" when
// it isn't at one.
func (in hintInput) attributeAt() (string, int) {
	if in.src == nil || in.from < 0 || in.from >= len(in.src) {
		return "", 0
	}
	i := in.from
	if in.src[i] != '@' && i > 0 && in.src[i-1] == '@' {
		i--
	}
	if in.src[i] != '@' {
		return "", 0
	}
	j := i + 1
	for j < len(in.src) && (isIdentByte(in.src[j]) || in.src[j] == '.' && j+1 < len(in.src) && isIdentByte(in.src[j+1])) {
		j++
	}
	return string(in.src[i:j]), i
}

// identEnd is the end of the identifier the error starts at.
func (in hintInput) identEnd() int {
	if in.src == nil || in.from < 0 {
		return 0
	}
	j := in.from
	for j < len(in.src) && isIdentByte(in.src[j]) {
		j++
	}
	return j
}

// frameworkPackages are the import paths of the packages of the vuka
// toolchain set, by name, each in the module that holds it.
var frameworkPackages = map[string][2]string{
	"web":     {"github.com/vuka-lang/web/v2", "github.com/vuka-lang/web/v2"},
	"graphql": {"github.com/vuka-lang/web/v2/graphql", "github.com/vuka-lang/web/v2"},
	"auth":    {"github.com/vuka-lang/web/v2/auth", "github.com/vuka-lang/web/v2"},
	"mail":    {"github.com/vuka-lang/web/v2/mail", "github.com/vuka-lang/web/v2"},
	"cron":    {"github.com/vuka-lang/web/v2/cron", "github.com/vuka-lang/web/v2"},
	"ui":      {"github.com/vuka-lang/ui/v2", "github.com/vuka-lang/ui/v2"},
	"orm":     {ormModule, ormModule},
	"vuka":    {"github.com/vuka-lang/vuka/v2", "github.com/vuka-lang/vuka/v2"},
}

// importFor is the one package named name the error's file can mean: one
// the package's other files import by that name, else the standard
// library's, else a vuka package of a module go.mod requires; "" when none
// or several.
func (in hintInput) importFor(name string) string {
	if t := in.types; t != nil {
		var found []string
		for _, f := range t.files {
			for _, s := range f.ast.Imports {
				p, _ := strconv.Unquote(s.Path.Value)
				if importName(f.ast, p) == name && !slices.Contains(found, p) {
					found = append(found, p)
				}
			}
		}
		if len(found) == 1 {
			return found[0]
		}
		if len(found) > 1 {
			return ""
		}
	}
	if bp, err := build.Default.Import(name, "", 0); err == nil && bp.Goroot && bp.Name == name {
		return name
	}
	if fw, ok := frameworkPackages[name]; ok && in.requires(fw[1]) {
		return fw[0]
	}
	return ""
}

// requires reports whether the module's go.mod requires mod, or is it.
func (in hintInput) requires(mod string) bool {
	if in.modPath == mod {
		return true
	}
	b, err := os.ReadFile(filepath.Join(in.modRoot, "go.mod"))
	return err == nil && regexp.MustCompile(`(?m)^\s*(require\s+)?`+regexp.QuoteMeta(mod)+`\s+v`).Match(b)
}

// typedFields explains an undefined XFields: the orm's typed fields of the
// model X, which vuka has the orm generate for the build.
func (in hintInput) typedFields(h *hint, qual, name, model string) {
	t := in.types
	if t == nil {
		return
	}
	scope := t.pkg.Scope()
	shown := model
	if qual != "" {
		pn, ok := t.info.Uses[qualIdent(t.path, qual)].(*types.PkgName)
		if !ok {
			return
		}
		scope, shown = pn.Imported().Scope(), qual+"."+model
	}
	tn, ok := scope.Lookup(model).(*types.TypeName)
	if !ok {
		return
	}
	if !isModel(tn.Type()) {
		if in.requires(ormModule) {
			h.note = name + " would be the orm's typed fields of " + shown + ", and " + shown + " isn't a model: it doesn't embed orm.Base"
		}
		return
	}
	h.note = name + " is generated by the orm for " + shown + " and wasn't generated for this build"
	if !in.readsField() {
		example := strings.TrimSuffix(shown, model) + name
		if f := firstField(tn.Type()); f != "" {
			example += "." + f
		}
		h.help = "vuka has the orm generate it for a build that reads one of its fields, as " + example + " does; go tool orm generate writes it to disk"
	}
}

// qualIdent is the identifier qual of the selector the error is at.
func qualIdent(path []ast.Node, qual string) *ast.Ident {
	for i := len(path) - 1; i >= 0; i-- {
		if sel, ok := path[i].(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == qual {
				return id
			}
		}
	}
	return nil
}

// readsField reports whether the selector the error is at is read for a
// field of it (UserFields.Email).
func (in hintInput) readsField() bool {
	t := in.types
	for i := len(t.path) - 1; i > 0; i-- {
		if _, ok := t.path[i].(*ast.SelectorExpr); ok {
			outer, ok := t.path[i-1].(*ast.SelectorExpr)
			return ok && outer.X == t.path[i]
		}
	}
	return false
}

// firstField is the name of a struct's first exported field of its own.
func firstField(t types.Type) string {
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return ""
	}
	for f := range st.Fields() {
		if !f.Embedded() && f.Exported() {
			return f.Name()
		}
	}
	return ""
}

// isModel reports whether t is an orm model: a struct embedding orm.Base.
func isModel(t types.Type) bool {
	st, ok := t.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for f := range st.Fields() {
		if !f.Embedded() {
			continue
		}
		ft := types.Unalias(f.Type())
		if p, ok := ft.(*types.Pointer); ok {
			ft = types.Unalias(p.Elem())
		}
		if n, ok := ft.(*types.Named); ok && n.Obj().Pkg() != nil && strings.HasPrefix(n.Obj().Pkg().Path(), ormModule) && n.Obj().Name() == "Base" {
			return true
		}
	}
	return false
}

// importedDir is the directory of the package the error's file imports as
// qual, "" when unknown.
func (in hintInput) importedDir(qual string) string {
	t := in.types
	if t == nil {
		return ""
	}
	pn, ok := t.info.Uses[qualIdent(t.path, qual)].(*types.PkgName)
	if !ok || in.modPath == "" {
		return ""
	}
	p := pn.Imported().Path()
	rel, ok := strings.CutPrefix(p, in.modPath+"/")
	if !ok {
		return ""
	}
	return filepath.Join(in.modRoot, filepath.FromSlash(rel))
}

// urlsDir reports whether dir is the package urls vuka generates for the
// folder routes.
func (in hintInput) urlsDir(dir string) bool {
	return filepath.Base(dir) == urlsDirName && filepath.Base(filepath.Dir(dir)) == pagesDirName
}

var urlsNoField = regexp.MustCompile(`\(type \*?urls\.\w+ has no field or method (\w+)`)

// noField explains a field or method that isn't there, of a value of the
// generated urls.
func (in hintInput) noField(h *hint, first string) {
	m := urlsNoField.FindStringSubmatch(first)
	if m == nil || in.modRoot == "" || !fileExists(filepath.Join(in.modRoot, pagesDirName, urlsDirName)) {
		return
	}
	h.note = "the urls package is generated by vuka from the folders under pages/, and no page or folder is " + m[1] + " there (vuka routes lists them)"
}
