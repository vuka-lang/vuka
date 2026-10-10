package transpile

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"
	"strconv"
	"strings"
)

// runtimeNames are the names Vuka code uses unqualified for the runtime package.
var runtimeNames = map[string]bool{"Result": true, "Option": true, "Ok": true, "Err": true, "Some": true, "None": true}

// engine lowers a package in rounds. Each round rebuilds every file's text from
// the source, the rewrites decided so far, and placeholders for the rest; parses
// and type-checks it; and decides whatever has become decidable. A construct is
// decidable once the types it depends on are known: in `x := f()?; match x {…}`
// the match waits for the round in which x has a type.
type engine struct {
	files, vuka   []*fileState
	imp           types.Importer
	errs          *ErrorList
	bare          bool
	dir           string
	importPath    string
	templ         func(string) (TemplFile, error)
	templRegistry string
	tplImports    map[string]string         // packages of .templ files in subdirectories → their names in generated code
	nfiles        int                       // embedded files so far, naming their variables
	targets       map[string]*types.Package // import paths → the JSX target there, or nil

	declared      map[string]bool // package-level names
	typeNames     map[string]bool // package-level type names
	genericEmbeds map[string]bool // types embedding a generic or another package's type
	members       map[string]bool // Type.field and Type.method names
	methodNames   map[string]bool // Type.method names
	typed         bool            // the package is type-checked every round
	sets          map[string]*overloadSet
	funcs         map[string]*overloadSet
	methods       map[string]*overloadSet
	mnames        map[string]bool
	pending       map[*fileState]map[int]string // runtime calls whose type can't be inferred yet
	refErrs       map[*fileState]*refErrs       // field references naming no field, so far

	fset     *token.FileSet
	pkg      *types.Package
	info     *types.Info
	typeErrs []types.Error
	progress bool
}

func (e *engine) run() {
	e.pending = map[*fileState]map[int]string{}
	e.refErrs = map[*fileState]*refErrs{}
	for round := 0; round < 100; round++ {
		e.progress = false
		if !e.parseAll(round == 0) {
			return
		}
		if round == 0 {
			e.prepare()
			if len(*e.errs) > 0 {
				return
			}
		}
		e.qualify()
		if e.progress {
			continue
		}
		if !e.needsTypes() {
			return
		}
		e.check()
		if e.oldRuntime() {
			return
		}
		for _, f := range e.vuka {
			for _, t := range f.tries {
				t.fresh = false
			}
		}
		if e.inferStatics() {
			// Uses of those statics type-check against their real types first.
			e.progress = true
			continue
		}
		for _, f := range e.vuka {
			embedded := e.embedFiles(f)
			e.classify(f)
			if embedded {
				e.render(f)
				e.progress = true
			}
			e.resolveStatics(f)
			e.resolveFieldRefs(f)
			e.selfCalls(f)
			e.resolveCalls(f)
			e.infer(f)
			e.lowerJSX(f)
			e.checkEvents(f)
			for _, t := range f.tries {
				if !t.done && !t.dead {
					e.lowerTry(f, t)
				}
			}
			for _, m := range f.matches {
				if !m.done && !m.dead {
					e.lowerMatch(f, m)
				}
			}
		}
		if len(*e.errs) > 0 || !e.progress {
			break
		}
	}
	if len(*e.errs) == 0 && e.info != nil {
		e.staticCycles()
	}
	if len(*e.errs) == 0 && e.info != nil {
		e.report()
	}
}

func (e *engine) parseAll(first bool) bool {
	e.fset = token.NewFileSet()
	for _, f := range e.files {
		if f.vuka {
			f.build()
		} else {
			f.cur, f.text, f.body = nil, f.src, len(f.src)
		}
		if !f.parse(e.fset, e.errs) {
			if !first && f.vuka {
				e.errs.add(f.at(0), "vuka produced Go that doesn't parse; please report this")
			}
			return false
		}
		if first && f.vuka {
			f.attach()
			f.pkgEnd = f.orig(f.ast.Name.End())
			for _, imp := range f.ast.Imports {
				if imp.Name != nil && imp.Name.Name == "." {
					f.dotImps = true
				}
			}
		}
	}
	return true
}

// prepare collects the package-level names and the overloaded functions.
func (e *engine) prepare() {
	e.declared, e.typeNames = map[string]bool{}, map[string]bool{}
	for _, f := range e.files {
		for _, d := range f.ast.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					e.declared[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						e.declared[s.Name.Name] = true
						e.typeNames[s.Name.Name] = true
					case *ast.ValueSpec:
						for _, n := range s.Names {
							e.declared[n.Name] = true
						}
					}
				}
			}
		}
	}
	e.sets = findOverloads(e.vuka, e.errs)
	e.funcs, e.methods, e.mnames = map[string]*overloadSet{}, map[string]*overloadSet{}, map[string]bool{}
	for key, s := range e.sets {
		if s.recv == "" {
			e.funcs[s.name] = s
		} else {
			e.methods[key] = s
			e.mnames[s.name] = true
		}
	}
	for _, f := range e.files {
		if f.vuka {
			continue
		}
		for _, d := range f.ast.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				if s := e.sets[declKey(fd)]; s != nil {
					e.errs.add(e.fset.Position(fd.Name.Pos()), "%s is overloaded in Vuka source; a .go file can't declare it too", s.display())
				}
			}
		}
	}
	for _, f := range e.vuka {
		*e.errs = append(*e.errs, f.staticErrs...)
	}
	e.fixStaticFuncs()
	e.inferSelf()
	if e.decorate() || len(e.sets) > 0 {
		e.progress = true
	}
	for _, f := range e.vuka {
		if len(f.statics) > 0 || len(f.staticFuncs) > 0 {
			e.progress = true
		}
	}
}

// qualify points Result, Option, Ok, Err, Some and None at the runtime package
// wherever the package doesn't declare its own.
func (e *engine) qualify() {
	for _, f := range e.vuka {
		if f.dotImps {
			continue
		}
		for _, id := range f.ast.Unresolved {
			if !runtimeNames[id.Name] || e.declared[id.Name] || !f.lowerable(id.Pos()) {
				continue
			}
			off := f.orig(id.Pos())
			if f.done[off] {
				continue
			}
			f.done[off] = true
			f.insert(off, f.runtime()+".", 1)
			e.progress = true
		}
	}
}

// runtime is the name the runtime package goes by in f, importing it on first use.
func (f *fileState) runtime() string {
	if f.rt != "" {
		return f.rt
	}
	for _, imp := range f.ast.Imports {
		if path, _ := strconv.Unquote(imp.Path.Value); path == RuntimePath {
			f.rt = "vuka"
			if imp.Name != nil {
				f.rt = imp.Name.Name
			}
			return f.rt
		}
	}
	f.rt = "vuka"
	f.insert(f.pkgEnd, `; import vuka "`+RuntimePath+`"`, 0)
	return f.rt
}

func (e *engine) needsTypes() bool {
	if e.typed || len(e.sets) > 0 {
		return true
	}
	e.typed = e.needsTypesNow()
	return e.typed
}

// needsTypesNow is whether anything in the package, as it is this round, needs
// type information. Once it has, the package stays type-checked: a rewrite can
// remove the construct that needed it while what follows still needs types.
func (e *engine) needsTypesNow() bool {
	for _, f := range e.vuka {
		if f.rt != "" || f.mayEmbed() || len(f.tries) > 0 || len(f.matches) > 0 || len(f.jsx) > 0 || len(f.statics) > 0 || len(f.staticFuncs) > 0 || e.mayUseStatics(f) || e.mayUseFieldRefs(f) {
			return true
		}
	}
	return false
}

func (e *engine) check() {
	e.typeErrs = nil
	conf := types.Config{
		Importer:    e.imp,
		FakeImportC: true,
		Error:       func(err error) { e.typeErrs = append(e.typeErrs, err.(types.Error)) },
	}
	e.info = &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	var files []*ast.File
	for _, f := range e.files {
		files = append(files, f.ast)
	}
	e.pkg, _ = conf.Check(files[0].Name.Name, e.fset, files, e.info)
	for _, s := range e.sets {
		for _, o := range s.list {
			o.sig = nil
			for _, f := range e.vuka {
				if f != o.file {
					continue
				}
				for _, d := range f.ast.Decls {
					if fd, ok := d.(*ast.FuncDecl); ok && f.orig(fd.Name.Pos()) == o.nameOff {
						if fn, ok := e.info.Defs[fd.Name].(*types.Func); ok {
							o.sig = fn.Type().(*types.Signature)
						}
					}
				}
			}
		}
	}
}

func (e *engine) fileOf(p token.Pos) *fileState {
	tf := e.fset.File(p)
	for _, f := range e.files {
		if f.tf == tf {
			return f
		}
	}
	return nil
}

func (e *engine) typeErrPos(err types.Error) token.Position {
	if f := e.fileOf(err.Pos); f != nil && f.vuka {
		return f.pos(f.off(err.Pos))
	}
	return err.Fset.Position(err.Pos)
}

// report explains every construct no round could lower.
func (e *engine) report() {
	stuck := false
	for _, f := range e.vuka {
		for _, t := range f.tries {
			if !t.done && !t.dead {
				stuck = true
				e.errs.add(f.at(t.off), "can't lower ?: %s", or(t.fail, "the operand's type is unknown"))
			}
		}
		for _, m := range f.matches {
			if !m.done && !m.dead {
				stuck = true
				e.errs.add(f.at(m.start), "can't lower match: %s", or(m.fail, "the subject's type is unknown"))
			}
		}
		for _, t := range f.jsx {
			for _, c := range t.comps {
				if !t.done && !c.resolved && !c.dead {
					stuck = true
					e.errs.add(f.at(c.el.start), "can't lower <%s>: %s", c.el.tag, or(c.fail, "its type is unknown"))
				}
			}
		}
		for _, s := range f.statics {
			if s.infer {
				stuck = true
				e.errs.add(f.at(s.nameOff), "can't infer the type of static %s.%s; write it: static %s T = …", s.typeName, s.name, s.name)
			}
		}
		if re := e.refErrs[f]; re != nil {
			stuck = true
			*e.errs = append(*e.errs, re.errs...)
		}
		for off, name := range e.pending[f] {
			stuck = true
			e.errs.add(f.at(off), "can't infer the type of %s here; write %s[T]%s", name, name, map[bool]string{true: "()", false: "(…)"}[name == "None"])
		}
	}
	if e.unresolvedOverloads() {
		stuck = true
	}
	if !stuck {
		return
	}
	reported := map[token.Position]bool{}
	for _, err := range *e.errs {
		reported[err.Pos] = true
	}
	for _, re := range e.refErrs {
		maps.Copy(reported, re.hide)
	}
	shown := 0
	for _, te := range e.typeErrs {
		if shown == 5 || reported[e.typeErrPos(te)] || e.mentionsOverload(te.Msg) || strings.Contains(te.Msg, "declared and not used") ||
			strings.Contains(te.Msg, "cannot infer T") {
			continue
		}
		pos, msg := e.typeErrPos(te), te.Msg
		if f := e.fileOf(te.Pos); f != nil && f.vuka {
			m := SourceMap{spellings: f.spellings}
			msg = m.Message(pos.Line, msg)
		}
		e.errs.add(pos, "%s", msg)
		shown++
	}
}

// oldRuntime reports, as one clear error, generated code using runtime names
// the module's required runtime version lacks.
func (e *engine) oldRuntime() bool {
	for _, te := range e.typeErrs {
		f := e.fileOf(te.Pos)
		if f == nil || !f.vuka || f.rt == "" {
			continue
		}
		name, ok := strings.CutPrefix(te.Msg, "undefined: "+f.rt+".")
		if !ok {
			continue
		}
		e.errs.add(e.typeErrPos(te), "this module requires a Vuka runtime without %s.%s; update it: go get %s@%s",
			f.rt, name, RuntimePath, RuntimeVersion)
		return true
	}
	return false
}

// display writes t the way Vuka source spells it, for messages.
func (e *engine) display(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		if p == e.pkg || p.Path() == RuntimePath {
			return ""
		}
		return p.Name()
	})
}

func or(s, def string) string {
	if s != "" {
		return s
	}
	return def
}

// enclosingFunc is the signature and AST of the function n is in.
func (e *engine) enclosingFunc(f *fileState, n ast.Node) (*types.Signature, *ast.FuncType) {
	for p := f.parents[n]; p != nil; p = f.parents[p] {
		switch p := p.(type) {
		case *ast.FuncLit:
			if sig, ok := e.info.Types[p].Type.(*types.Signature); ok {
				return sig, p.Type
			}
			return nil, nil
		case *ast.FuncDecl:
			if fn, ok := e.info.Defs[p.Name].(*types.Func); ok {
				return fn.Type().(*types.Signature), p.Type
			}
			return nil, nil
		}
	}
	return nil, nil
}

// qualifier names packages the way f imports them. A package f doesn't import is
// recorded in missing.
func (e *engine) qualifier(f *fileState, missing *string) types.Qualifier {
	return func(p *types.Package) string {
		if p == e.pkg {
			return ""
		}
		if p.Path() == RuntimePath {
			return f.runtime()
		}
		for _, imp := range f.ast.Imports {
			if path, _ := strconv.Unquote(imp.Path.Value); path == p.Path() {
				if imp.Name == nil {
					return p.Name()
				}
				if imp.Name.Name == "." {
					return ""
				}
				return imp.Name.Name
			}
		}
		*missing = p.Path()
		return p.Name()
	}
}

// typeText writes t as f can spell it.
func (e *engine) typeText(f *fileState, t types.Type) (string, string) {
	missing := ""
	s := types.TypeString(t, e.qualifier(f, &missing))
	if missing != "" {
		return "", "the type " + s + " needs an import of " + strconv.Quote(missing)
	}
	return s, ""
}

// runtimeType reports whether t is the runtime's Result or Option, and its
// type argument.
func runtimeType(t types.Type) (string, types.Type) {
	n, ok := types.Unalias(t).(*types.Named)
	if !ok || n.Obj().Pkg() == nil || n.Obj().Pkg().Path() != RuntimePath || n.TypeArgs().Len() != 1 {
		return "", nil
	}
	switch name := n.Obj().Name(); name {
	case "Result", "Option":
		return name, n.TypeArgs().At(0)
	}
	return "", nil
}

func isInvalid(t types.Type) bool {
	if t == nil {
		return true
	}
	b, ok := t.(*types.Basic)
	return ok && b.Kind() == types.Invalid
}

var errorType = types.Universe.Lookup("error").Type()

// zero is the zero value of t, written for f.
func (e *engine) zero(f *fileState, t types.Type) (string, string) {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch {
		case u.Info()&types.IsBoolean != 0:
			return "false", ""
		case u.Info()&types.IsString != 0:
			return `""`, ""
		case u.Info()&types.IsNumeric != 0:
			return "0", ""
		case u.Kind() == types.UnsafePointer:
			return "nil", ""
		}
	case *types.Pointer, *types.Slice, *types.Map, *types.Chan, *types.Signature, *types.Interface:
		if _, ok := t.(*types.TypeParam); !ok {
			return "nil", ""
		}
	}
	s, problem := e.typeText(f, t)
	if problem != "" {
		return "", problem
	}
	switch t.Underlying().(type) {
	case *types.Struct, *types.Array:
		if _, ok := t.(*types.TypeParam); !ok {
			return s + "{}", ""
		}
	}
	return "*new(" + s + ")", ""
}
