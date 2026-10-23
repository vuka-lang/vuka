package transpile

import (
	"go/ast"
	"go/scanner"
	"go/token"
	"go/types"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// WebPath is the import path of the package holding Var, which a context
// processor's variable is read with.
const WebPath = "github.com/vuka-lang/web/v2"

// Processor is a context processor: a function of another package an app's
// main lists in web.ContextProcessors(…). Each exported field of the
// struct it returns is a variable its templates read by name.
type Processor struct {
	Path string // the function's package
	Func string // its name
	// Pos is its entry in the list; End the entry's end, for an editor.
	Pos token.Position
	End token.Position
	// The wrappers it is listed in: web.Browser(p), web.Personal(p),
	// web.Auth(p, …).
	Browser, Personal, Auth bool
}

// Display is p as main writes it: users.Context.
func (p Processor) Display() string { return PackageName(p.Path) + "." + p.Func }

// PackageName is the name a package's import path suggests: its last
// element, a major version suffix (/v2) skipped.
func PackageName(path string) string {
	elems := strings.Split(path, "/")
	last := elems[len(elems)-1]
	if len(elems) > 1 && len(last) > 1 && last[0] == 'v' && strings.Trim(last[1:], "0123456789") == "" {
		last = elems[len(elems)-2]
	}
	return last
}

// Context is the context processors of one package main.
type Context struct {
	Main       string // the main package's import path
	Dir        string // its directory, for messages
	Processors []Processor
}

// Var is a context processor's variable: a field of its result.
type Var struct {
	Name      string // the template's name: the field's, lower camel case
	Field     *types.Var
	Type      types.Type
	Func      *types.Func // the processor
	Processor Processor
}

// LowerCamel is the variable name of a field: its leading capitals
// lowered, the last of several kept when a lower-case letter follows,
// CSRFToken → csrfToken, ID → id.
func LowerCamel(name string) string {
	rs := []rune(name)
	n := 0
	for n < len(rs) && unicode.IsUpper(rs[n]) {
		n++
	}
	if n > 1 && n < len(rs) && unicode.IsLower(rs[n]) {
		n--
	}
	for i := 0; i < n; i++ {
		rs[i] = unicode.ToLower(rs[i])
	}
	return string(rs)
}

// ContextVars are the variables of c's processors, by name, loaded with
// imp, and the problems of the list: a processor that isn't one (E0425)
// and two providing one name (E0421). A processor whose package doesn't
// load is left out, its own errors explaining it, and so is one of the
// package self, being transpiled: it reads its own with web.Var.
func ContextVars(c Context, imp types.Importer, self string) (map[string]*Var, ErrorList) {
	var errs ErrorList
	vars := map[string]*Var{}
	for _, p := range c.Processors {
		if p.Path == c.Main {
			errs.add("E0425", p.Pos, "%s is declared in package main: a context processor is declared in a package of its own, which the packages main imports can see", p.Func).
				help("move it to a package such as users, and list users.%s", p.Func)
			continue
		}
		if p.Path == self {
			continue
		}
		pkg, err := imp.Import(p.Path)
		if err != nil || pkg == nil {
			continue
		}
		fn, _ := pkg.Scope().Lookup(p.Func).(*types.Func)
		st, why := processorResult(fn)
		if why != "" {
			errs.add("E0425", p.Pos, "%s isn't a context processor: %s", p.Display(), why).
				help("a context processor is func(ctx context.Context, deps…) (S, error), S a struct whose exported fields are its variables")
			continue
		}
		var clash []string
		var with *Var
		for i := 0; i < st.NumFields(); i++ {
			fl := st.Field(i)
			if !fl.Exported() || fl.Embedded() {
				continue
			}
			v := &Var{Name: LowerCamel(fl.Name()), Field: fl, Type: fl.Type(), Func: fn, Processor: p}
			if prev := vars[v.Name]; prev != nil {
				if with == nil || with.Processor == prev.Processor {
					with = prev
					clash = append(clash, v.Name)
				}
				continue
			}
			vars[v.Name] = v
		}
		if with != nil && with.Processor.Path == p.Path && with.Processor.Func == p.Func {
			errs.add("E0421", p.Pos, "%s is listed twice", p.Display()).help("list it once")
		} else if with != nil {
			prev := with.Processor
			errs.add("E0421", p.Pos, "%s and %s both provide %s", prev.Display(), p.Display(), strings.Join(clash, ", ")).
				help("%s is listed at %s:%d; rename the fields of one result: a template reads each variable by its name, and which one it means can't be told", prev.Display(), prev.Pos.Filename, prev.Pos.Line)
		}
	}
	return vars, errs
}

// processorResult is the struct a context processor returns, or why fn
// isn't one.
func processorResult(fn *types.Func) (*types.Struct, string) {
	if fn == nil {
		return nil, "no such function"
	}
	sig := fn.Type().(*types.Signature)
	if sig.Recv() != nil || sig.TypeParams().Len() > 0 {
		return nil, "it is a method or generic"
	}
	if sig.Params().Len() == 0 || !isContext(sig.Params().At(0).Type()) {
		return nil, "its first parameter isn't a context.Context"
	}
	if sig.Results().Len() != 2 || !types.Identical(sig.Results().At(1).Type(), errorType) {
		return nil, "it doesn't return (S, error)"
	}
	st, ok := sig.Results().At(0).Type().Underlying().(*types.Struct)
	if !ok {
		return nil, "what it returns isn't a struct"
	}
	return st, ""
}

// ctxVars are the context processors' variables a package reads by name:
// those of each package main linking it in.
type ctxVars struct {
	mains []Context
	vars  []map[string]*Var
	names map[string]bool // in any main's
}

// loadContexts reads the processors of the mains linking the package in.
func (e *engine) loadContexts(cs []Context) {
	if len(cs) == 0 {
		return
	}
	cv := &ctxVars{names: map[string]bool{}}
	for _, c := range cs {
		vars, errs := ContextVars(c, e.imp, e.importPath)
		*e.errs = append(*e.errs, errs...)
		cv.mains = append(cv.mains, c)
		cv.vars = append(cv.vars, vars)
		for n := range vars {
			cv.names[n] = true
		}
	}
	e.ctx = cv
}

// lookup is the variable name every main provides with one type, or why
// it isn't one; nil and "" for a name none provides.
func (cv *ctxVars) lookup(name string) (*Var, string) {
	var v *Var
	var lacking []string
	for i, vars := range cv.vars {
		w := vars[name]
		if w == nil {
			lacking = append(lacking, cv.mains[i].Main)
			continue
		}
		if v != nil && !types.Identical(v.Type, w.Type) {
			return nil, name + " is " + types.TypeString(v.Type, nil) + " from " + v.Processor.Display() + " in " + cv.mains[0].Main +
				" but " + types.TypeString(w.Type, nil) + " from " + w.Processor.Display() + " in " + cv.mains[i].Main
		}
		if v == nil {
			v = w
		}
	}
	if v == nil {
		return nil, ""
	}
	if len(lacking) > 0 {
		return v, "the web.ContextProcessors of " + strings.Join(lacking, ", ") + " doesn't list " + v.Processor.Display() + ", which provides " + name
	}
	return v, ""
}

// sortedNames are the variable names, for suggestions.
func (cv *ctxVars) sortedNames() []string {
	out := make([]string, 0, len(cv.names))
	for n := range cv.names {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// mayRead reports whether src names a processor variable, as an
// identifier not after a dot: a file that doesn't needs nothing done.
func (cv *ctxVars) mayRead(src []byte) bool {
	if cv == nil || len(cv.names) == 0 {
		return false
	}
	return namesIdent(src, cv.names)
}

// namesIdent reports whether src has an identifier of names, not after a dot.
func namesIdent(src []byte, names map[string]bool) bool {
	var s scanner.Scanner
	fs := token.NewFileSet()
	s.Init(fs.AddFile("", -1, len(src)), src, func(token.Position, string) {}, 0)
	prev := token.ILLEGAL
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			return false
		}
		if tok == token.IDENT && prev != token.PERIOD && names[lit] {
			return true
		}
		prev = tok
	}
}

// lowerContextVars lowers each bare name of f that names no declaration
// and is a processor variable: me is web.Var[*users.User](ctx, "me"),
// ctx being the one in scope there — a template's own, the method's
// parameter.
func (e *engine) lowerContextVars(f *fileState) {
	cv := e.ctx
	if cv == nil || f.ast == nil || !f.readsVars {
		return
	}
	for _, id := range f.ast.Unresolved {
		if !cv.names[id.Name] || e.info.Uses[id] != nil || e.info.Defs[id] != nil || !f.lowerable(id.Pos()) {
			continue
		}
		off := f.orig(id.Pos())
		if f.done[off] || f.shapeName(off, id.Name) || e.fieldKey(f, id) {
			continue
		}
		v, why := cv.lookup(id.Name)
		if why != "" {
			f.done[off] = true
			e.errs.add("E0422", f.at(off), "%s", why).help("list the processor in each main the package is linked into, with the same type")
			continue
		}
		if !e.ctxInScope(id) {
			f.done[off] = true
			e.errs.add("E0423", f.at(off), "%s is a variable of the context processor %s: reading it takes a ctx, and there is none here", id.Name, v.Processor.Display()).
				help("take ctx context.Context as the method's first parameter, or read it in the markup")
			continue
		}
		typ := e.typeTextAuto(f, v.Type)
		web := e.webName(f)
		pre := web + ".Var[" + typ + "](ctx, \""
		post := "\")"
		f.done[off] = true
		f.fixed = append(f.fixed, edit{start: off, end: off + len(id.Name), text: pre + id.Name + post,
			segs: []segment{{genLen: len(pre), src: off}, {gen: len(pre), genLen: len(id.Name), src: off, srcLen: len(id.Name), copy: true}, {gen: len(pre) + len(id.Name), genLen: len(post), src: off + len(id.Name)}}})
		f.spell(pre+id.Name+post, id.Name, off)
		f.varReads = append(f.varReads, VarRead{Off: off, Name: id.Name, Type: e.display(v.Type), Processor: v.Processor})
		e.progress = true
	}
}

// fieldKey reports whether id is a struct literal's field name.
func (e *engine) fieldKey(f *fileState, id *ast.Ident) bool {
	kv, ok := f.parents[id].(*ast.KeyValueExpr)
	if !ok || kv.Key != id {
		return false
	}
	lit, ok := f.parents[kv].(*ast.CompositeLit)
	if !ok {
		return false
	}
	if tv, ok := e.info.Types[lit]; ok && tv.Type != nil {
		_, st := tv.Type.Underlying().(*types.Struct)
		return st
	}
	return true
}

// ctxInScope reports whether a context.Context named ctx is in scope at id.
func (e *engine) ctxInScope(id *ast.Ident) bool {
	s := e.pkg.Scope().Innermost(id.Pos())
	if s == nil {
		return false
	}
	_, obj := s.LookupParent("ctx", id.Pos())
	return obj != nil && isContext(obj.Type())
}

// webName is the name f imports web as, importing it on first use.
func (e *engine) webName(f *fileState) string {
	missing := ""
	q := e.qualifier(f, &missing)(types.NewPackage(WebPath, "web"))
	if missing != "" {
		return f.importAs(WebPath, "__web")
	}
	return q
}

// undefinedVars reports each name of a file that names nothing and is
// near a processor variable's (E0424): a typo for one.
func (e *engine) undefinedVars() {
	cv := e.ctx
	if cv == nil || len(cv.names) == 0 {
		return
	}
	undefined := map[token.Position]bool{}
	for _, te := range e.typeErrs {
		if strings.HasPrefix(te.Msg, "undefined: ") {
			undefined[e.typeErrPos(te)] = true
		}
	}
	names := cv.sortedNames()
	for _, f := range e.vuka {
		if f.ast == nil {
			continue
		}
		for _, id := range f.ast.Unresolved {
			if cv.names[id.Name] || e.info.Uses[id] != nil || e.info.Defs[id] != nil || !f.lowerable(id.Pos()) {
				continue
			}
			off := f.orig(id.Pos())
			pos := f.at(off)
			if f.done[off] || !undefined[pos] {
				continue
			}
			s := closestName(id.Name, names)
			if s == "" {
				continue
			}
			f.done[off] = true
			v, _ := cv.lookup(s)
			from := ""
			if v != nil {
				from = " (from " + v.Processor.Display() + ")"
			}
			err := e.errs.add("E0424", pos, "undefined: %s", id.Name).suggest(s + from)
			err.Len = len(id.Name)
		}
	}
}

// templateNames are the processor variables a template's markup may read,
// for its implicit ctx.
func (e *engine) templateNames() map[string]bool {
	if e.ctx == nil {
		return nil
	}
	return e.ctx.names
}

// VarRead is a read of a context processor's variable: its offset in the
// file as written, its name and type, and the processor providing it.
// Off is -1 for a variable the file may read (Output.Available).
type VarRead struct {
	Off       int
	Name      string
	Type      string // as the package spells it: *users.User
	Processor Processor
}

// readsOf are f's lowered reads.
func (f *fileState) readsOf() []VarRead {
	out := slices.Clone(f.varReads)
	for i := range out {
		if f.html != nil {
			out[i].Off = f.html.toSrc(out[i].Off)
		}
	}
	slices.SortFunc(out, func(a, b VarRead) int { return a.Off - b.Off })
	return out
}

// available are the variables every main linking the package in provides.
func (e *engine) available() []VarRead {
	if e.ctx == nil {
		return nil
	}
	var out []VarRead
	for _, n := range e.ctx.sortedNames() {
		if v, why := e.ctx.lookup(n); why == "" {
			out = append(out, VarRead{Off: -1, Name: n, Type: e.display(v.Type), Processor: v.Processor})
		}
	}
	return out
}
