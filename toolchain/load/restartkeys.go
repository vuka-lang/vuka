package load

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// RestartGoName is the generated file restart-only keys put in each main
// package: a call to web.RestartOnlyEnv with the keys the program reads only
// while it starts. vuka build, run, test and vet inject it through the
// overlay; vuka gen writes it to disk for plain go tools.
const RestartGoName = "vuka_restart_gen.go"

// RestartReport is what the analysis of a module found about the environment
// variables it reads with web.Env, web.MustEnv and web.Require.
type RestartReport struct {
	// Keys are the restart-only keys: read while the program starts, and not
	// handled by web.Hot or web.OnEnvChange. Sorted.
	Keys []string
	// Live are the keys read only by code that runs after the program has
	// started. Sorted.
	Live []string
	// Handled are keys read while the program starts that web.Hot or
	// web.OnEnvChange also names: the program deals with their changes. Sorted.
	Handled []string
	// Unclassified are the reads whose key isn't a string constant.
	Unclassified []token.Position
	// Web is the import path of web the module reads them with.
	Web string
}

// RestartKeys analyses the module at root for the keys it reads only while
// it starts (see analyzeRestart) and returns, for each main package, the
// generated file registering them. A main whose keys come out empty gets a
// file only when one is already on disk (gen wrote it earlier), so the stale
// one never breaks the build. The Go read is that of the module's own
// directories (hidden and _-prefixed ones, testdata, vendor, node_modules
// and nested modules left out) and the Go the compiler generated for its
// Vuka files (gens).
func RestartKeys(root, modPath string, pkgs []*Package, gens []Generated) ([]Generated, RestartReport, error) {
	root = realPath(root)
	byDir := map[string]string{}
	for _, p := range pkgs {
		byDir[p.Dir], byDir[p.BuildDir()] = p.ImportPath, p.ImportPath
	}
	importPath := func(dir string) string {
		if path, ok := byDir[dir]; ok {
			return path
		}
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel == "." {
			return modPath
		}
		return modPath + "/" + filepath.ToSlash(rel)
	}
	var srcs []restartSrc
	byTarget := map[string]bool{}
	for _, g := range gens {
		byTarget[g.Target] = true
		if !isTestFile(g.Target) && filepath.Base(g.Target) != RestartGoName {
			srcs = append(srcs, restartSrc{importPath(filepath.Dir(g.Target)), g.Target, g.Src})
		}
	}
	err := WalkDir(root, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if e.IsDir() {
			if name := e.Name(); path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
				name == "testdata" || name == "vendor" || name == "node_modules" ||
				hasGoMod(path) || fileExists(filepath.Join(path, Marker))) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || isTestFile(path) || byTarget[path] || filepath.Base(path) == RestartGoName {
			return nil
		}
		if src, err := os.ReadFile(path); err == nil {
			srcs = append(srcs, restartSrc{importPath(filepath.Dir(path)), path, src})
		}
		return nil
	})
	if err != nil {
		return nil, RestartReport{}, err
	}
	rep := analyzeRestartCached(srcs)
	keys := rep.Keys
	if len(keys) > 0 && !webRegistersRestart(root, rep.Web) {
		keys = nil
	}
	mains, err := mainPackages(root)
	if err != nil {
		return nil, rep, err
	}
	var out []Generated
	for _, m := range mains {
		if m.noLink {
			continue
		}
		target := filepath.Join(m.dir, RestartGoName)
		if len(keys) == 0 && !fileExists(target) {
			continue
		}
		out = append(out, Generated{Target: target, Src: restartSrcFile(RestartReport{Keys: keys, Web: rep.Web})})
	}
	return out, rep, nil
}

var restartProbe struct {
	sync.Mutex
	last map[string]bool
}

// webRegistersRestart reports whether the web package the module builds with
// has RestartOnlyEnv: an app on an older web is built as it always was, with
// nothing registered. It asks the go command where the package is, so a
// replace, a workspace or a vendor directory count, and reads its source.
func webRegistersRestart(root, webPath string) bool {
	key := webPath + "\x00" + root
	if fi, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		key += "\x00" + fi.ModTime().String()
	}
	restartProbe.Lock()
	defer restartProbe.Unlock()
	if ok, seen := restartProbe.last[key]; seen {
		return ok
	}
	c := exec.Command("go", "list", "-f", "{{.Dir}}", webPath)
	c.Dir = root
	out, err := c.Output()
	ok := false
	if err == nil {
		if dir := strings.TrimSpace(string(out)); dir != "" {
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if src, err := os.ReadFile(filepath.Join(dir, e.Name())); err == nil && strings.HasSuffix(e.Name(), ".go") && bytes.Contains(src, []byte("\nfunc RestartOnlyEnv(")) {
					ok = true
					break
				}
			}
		}
	}
	if restartProbe.last == nil {
		restartProbe.last = map[string]bool{}
	}
	restartProbe.last[key] = ok
	return ok
}

func isTestFile(name string) bool {
	return strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_test.vuka")
}

// restartSrcFile is the content of a main package's RestartGoName file.
func restartSrcFile(rep RestartReport) []byte {
	var b bytes.Buffer
	b.WriteString("// Code generated by vuka. DO NOT EDIT.\n\n")
	b.WriteString("// The environment variables the program reads only while it starts: a\n")
	b.WriteString("// change to one reaches it on restart (web.NeedsRestart).\n\npackage main\n")
	if len(rep.Keys) > 0 {
		fmt.Fprintf(&b, "\nimport vukaweb %q\n\nfunc init() {\n\tvukaweb.RestartOnlyEnv(\n", rep.Web)
		for _, k := range rep.Keys {
			fmt.Fprintf(&b, "\t\t%q,\n", k)
		}
		b.WriteString("\t)\n}\n")
	}
	return b.Bytes()
}

var lastRestart struct {
	sync.Mutex
	sum [sha256.Size]byte
	rep RestartReport
}

// analyzeRestartCached is analyzeRestart that remembers the last sources it
// analysed: an editor or a watching run builds often, and parsing the whole
// module each time is the cost.
func analyzeRestartCached(srcs []restartSrc) RestartReport {
	h := sha256.New()
	for _, s := range srcs {
		fmt.Fprintf(h, "%s\x00%s\x00%d\x00", s.pkgPath, s.file, len(s.src))
		h.Write(s.src)
	}
	var sum [sha256.Size]byte
	h.Sum(sum[:0])
	lastRestart.Lock()
	defer lastRestart.Unlock()
	if lastRestart.sum != sum {
		lastRestart.sum, lastRestart.rep = sum, analyzeRestart(srcs)
	}
	return lastRestart.rep
}

// restartSrc is one Go source file of a package.
type restartSrc struct {
	pkgPath, file string
	src           []byte
}

// webPrefix starts the import path of every major version of web.
const webPrefix = "github.com/vuka-lang/web/"

// startRegistrars are the names of the calls whose function arguments run
// once while the app is built or started: constructors a container builds
// once, and start hooks. The name is matched whatever it is called on.
var startRegistrars = []string{"Provide", "Eager", "Service", "Controller", "OnStart", "Check"}

type restartNode struct {
	reads  []restartRead
	calls  []*restartNode
	parent *restartNode // the function a literal is written in
	locals map[string]*restartNode
	pkg    *restartPkg
	file   *restartFile
	bodies []ast.Node
}

type restartRead struct {
	key     string
	literal bool
	always  bool // read when the app is built whatever calls it: web.Require
	pos     token.Pos
}

type restartFile struct {
	ast     *ast.File
	imports map[string]string // name used in the file -> import path
}

type restartPkg struct {
	path   string
	name   string
	funcs  map[string]*restartNode
	lits   map[string]*restartNode // package variables holding a function literal
	consts map[string]restartConst
}

// restartConst is a package constant's expression, read where it is written.
type restartConst struct {
	expr ast.Expr
	file *restartFile
}

type restartAnalysis struct {
	fset    *token.FileSet
	pkgs    map[string]*restartPkg
	methods map[string][]*restartNode
	lits    map[*ast.FuncLit]*restartNode
	roots   []*restartNode
	all     []*restartNode
	pending []*restartNode
	hot     map[string]bool
	web     string
	unclass []token.Position
}

// analyzeRestart finds the environment variables a module reads only while
// its program starts. It is syntactic, over the module's Go, and errs
// toward reporting a key:
//
// A read is a call of web.Env, web.MustEnv or web.Require with a string
// constant as its key (a literal, a constant of the package or another
// package of the module, or a concatenation of those). A read is at start
// when its function is reached from a root by calls: the package variable
// initialisers, init functions, main, and the Start method of any type (a
// service web starts). A call is resolved by name: f(), pkg.F() for a
// package of the module, and x.M() for every method of the module named M.
// A function value is called by what receives it only when it is a
// function literal called on the spot, or an argument of Provide, Eager,
// Service, Controller, OnStart or Check, which run it once while the app is
// built or started; any other function value (a handler, a cron's function,
// a returned closure) is not, however it is registered. web.Require checks
// when the app is built, so its keys are always at start.
//
// A key read at start is restart-only unless the module also names it in
// web.Hot or web.OnEnvChange, which handle its changes.
func analyzeRestart(srcs []restartSrc) RestartReport {
	a := &restartAnalysis{
		fset: token.NewFileSet(), pkgs: map[string]*restartPkg{}, methods: map[string][]*restartNode{},
		lits: map[*ast.FuncLit]*restartNode{}, hot: map[string]bool{},
	}
	for _, s := range srcs {
		f, err := parser.ParseFile(a.fset, s.file, s.src, parser.SkipObjectResolution)
		if err != nil || f == nil {
			continue
		}
		p := a.pkgs[s.pkgPath]
		if p == nil {
			p = &restartPkg{path: s.pkgPath, funcs: map[string]*restartNode{}, lits: map[string]*restartNode{}, consts: map[string]restartConst{}}
			a.pkgs[s.pkgPath] = p
		}
		p.name = f.Name.Name
		rf := &restartFile{ast: f, imports: map[string]string{}}
		for _, im := range f.Imports {
			path, _ := strconv.Unquote(im.Path.Value)
			name := ""
			if im.Name != nil {
				name = im.Name.Name
			} else {
				name = transpile.ImportName(path)
			}
			rf.imports[name] = path
			if strings.HasPrefix(path, webPrefix) && a.web == "" {
				a.web = path
			}
		}
		init := a.node(p, rf, nil)
		a.roots = append(a.roots, init)
		for _, d := range f.Decls {
			a.declare(p, rf, init, d)
		}
	}
	for len(a.pending) > 0 {
		n := a.pending[0]
		a.pending = a.pending[1:]
		a.walk(n)
	}

	inStart := map[*restartNode]bool{}
	queue := slices.Clone(a.roots)
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if inStart[n] {
			continue
		}
		inStart[n] = true
		queue = append(queue, n.calls...)
	}
	start, seen := map[string]bool{}, map[string]bool{}
	var rep RestartReport
	for _, n := range a.all {
		for _, r := range n.reads {
			if !r.literal {
				rep.Unclassified = append(rep.Unclassified, a.fset.Position(r.pos))
				continue
			}
			seen[r.key] = true
			if r.always || inStart[n] {
				start[r.key] = true
			}
		}
	}
	for k := range seen {
		switch {
		case !start[k]:
			rep.Live = append(rep.Live, k)
		case a.hot[k]:
			rep.Handled = append(rep.Handled, k)
		default:
			rep.Keys = append(rep.Keys, k)
		}
	}
	slices.Sort(rep.Keys)
	slices.Sort(rep.Live)
	slices.Sort(rep.Handled)
	slices.SortFunc(rep.Unclassified, func(x, y token.Position) int {
		if c := strings.Compare(x.Filename, y.Filename); c != 0 {
			return c
		}
		return x.Line - y.Line
	})
	rep.Web = a.web
	return rep
}

func (a *restartAnalysis) node(p *restartPkg, f *restartFile, parent *restartNode, bodies ...ast.Node) *restartNode {
	n := &restartNode{pkg: p, file: f, parent: parent, bodies: bodies}
	a.all = append(a.all, n)
	a.pending = append(a.pending, n)
	return n
}

func (a *restartAnalysis) lit(l *ast.FuncLit, p *restartPkg, f *restartFile, parent *restartNode) *restartNode {
	if n, ok := a.lits[l]; ok {
		return n
	}
	n := a.node(p, f, parent, l.Body)
	a.lits[l] = n
	return n
}

func (a *restartAnalysis) declare(p *restartPkg, f *restartFile, init *restartNode, d ast.Decl) {
	switch d := d.(type) {
	case *ast.FuncDecl:
		if d.Body == nil {
			return
		}
		n := a.node(p, f, nil, d.Body)
		switch {
		case d.Recv != nil:
			a.methods[d.Name.Name] = append(a.methods[d.Name.Name], n)
			if d.Name.Name == "Start" {
				a.roots = append(a.roots, n)
			}
		case d.Name.Name == "init":
			a.roots = append(a.roots, n)
		case d.Name.Name == "main" && p.name == "main":
			a.roots = append(a.roots, n)
		default:
			p.funcs[d.Name.Name] = n
		}
	case *ast.GenDecl:
		for _, s := range d.Specs {
			v, ok := s.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range v.Names {
				if i >= len(v.Values) {
					continue
				}
				if l, ok := v.Values[i].(*ast.FuncLit); ok && d.Tok == token.VAR {
					p.lits[name.Name] = a.lit(l, p, f, init)
				}
				if d.Tok == token.CONST {
					p.consts[name.Name] = restartConst{v.Values[i], f}
				}
			}
			if d.Tok == token.VAR {
				init.bodies = append(init.bodies, v)
			}
		}
	}
}

func stringConst(e ast.Expr) (string, bool) {
	switch e := e.(type) {
	case *ast.ParenExpr:
		return stringConst(e.X)
	case *ast.BasicLit:
		if e.Kind == token.STRING {
			s, err := strconv.Unquote(e.Value)
			return s, err == nil
		}
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			x, ok1 := stringConst(e.X)
			y, ok2 := stringConst(e.Y)
			return x + y, ok1 && ok2
		}
	}
	return "", false
}

// keyOf is the string an expression read in n's file is, when it is one the
// analysis can see: a literal, a constant of the package or of another
// package of the module, or a concatenation of those.
func (a *restartAnalysis) keyOf(e ast.Expr, n *restartNode) (string, bool) {
	return a.keyOfDepth(e, n, 0)
}

func (a *restartAnalysis) constOf(c restartConst, p *restartPkg, depth int) (string, bool) {
	return a.keyOfDepth(c.expr, &restartNode{pkg: p, file: c.file}, depth+1)
}

func (a *restartAnalysis) keyOfDepth(e ast.Expr, n *restartNode, depth int) (string, bool) {
	if depth > 8 {
		return "", false
	}
	switch e := e.(type) {
	case *ast.ParenExpr:
		return a.keyOfDepth(e.X, n, depth)
	case *ast.BasicLit:
		return stringConst(e)
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			x, ok1 := a.keyOfDepth(e.X, n, depth)
			y, ok2 := a.keyOfDepth(e.Y, n, depth)
			return x + y, ok1 && ok2
		}
	case *ast.Ident:
		if c, ok := n.pkg.consts[e.Name]; ok {
			return a.constOf(c, n.pkg, depth)
		}
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok {
			if p := a.pkgs[n.file.imports[x.Name]]; p != nil {
				if c, ok := p.consts[e.Sel.Name]; ok {
					return a.constOf(c, p, depth)
				}
			}
		}
	}
	return "", false
}

// local is the function literal a local variable of n, or of the function n
// is written in, was given.
func (a *restartAnalysis) local(n *restartNode, name string) (*restartNode, bool) {
	for ; n != nil; n = n.parent {
		if l, ok := n.locals[name]; ok {
			return l, true
		}
	}
	return nil, false
}

// walk records the reads and calls in a function's body.
func (a *restartAnalysis) walk(n *restartNode) {
	for _, body := range n.bodies {
		ast.Inspect(body, func(x ast.Node) bool {
			switch x := x.(type) {
			case *ast.FuncLit:
				a.lit(x, n.pkg, n.file, n)
				return false
			case *ast.AssignStmt:
				for i, lhs := range x.Lhs {
					if id, ok := lhs.(*ast.Ident); ok && i < len(x.Rhs) {
						if l, ok := x.Rhs[i].(*ast.FuncLit); ok {
							n.setLocal(id.Name, a.lit(l, n.pkg, n.file, n))
						}
					}
				}
			case *ast.ValueSpec:
				for i, id := range x.Names {
					if i < len(x.Values) {
						if l, ok := x.Values[i].(*ast.FuncLit); ok {
							n.setLocal(id.Name, a.lit(l, n.pkg, n.file, n))
						}
					}
				}
			case *ast.CallExpr:
				a.call(n, x)
			}
			return true
		})
	}
}

func (n *restartNode) setLocal(name string, l *restartNode) {
	if n.locals == nil {
		n.locals = map[string]*restartNode{}
	}
	n.locals[name] = l
}

func unwrapCallee(e ast.Expr) ast.Expr {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.IndexExpr:
			e = x.X
		case *ast.IndexListExpr:
			e = x.X
		default:
			return e
		}
	}
}

// call records what a call reads and which functions it runs.
func (a *restartAnalysis) call(n *restartNode, c *ast.CallExpr) {
	fun := unwrapCallee(c.Fun)
	name := ""
	fromWeb := false
	switch f := fun.(type) {
	case *ast.Ident:
		name = f.Name
	case *ast.SelectorExpr:
		name = f.Sel.Name
		if x, ok := f.X.(*ast.Ident); ok && strings.HasPrefix(n.file.imports[x.Name], webPrefix) {
			fromWeb = true
		}
	}
	if fromWeb {
		a.webCall(n, c, name)
	} else {
		a.edges(n, fun)
	}
	if slices.Contains(startRegistrars, name) {
		for _, arg := range c.Args {
			a.edges(n, unwrapCallee(arg))
		}
	}
}

// edges makes n call the functions e names or is.
func (a *restartAnalysis) edges(n *restartNode, e ast.Expr) {
	switch e := e.(type) {
	case *ast.FuncLit:
		n.calls = append(n.calls, a.lit(e, n.pkg, n.file, n))
	case *ast.Ident:
		if l, ok := a.local(n, e.Name); ok {
			n.calls = append(n.calls, l)
		} else if f := n.pkg.funcs[e.Name]; f != nil {
			n.calls = append(n.calls, f)
		} else if l := n.pkg.lits[e.Name]; l != nil {
			n.calls = append(n.calls, l)
		}
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok {
			if path, imported := n.file.imports[x.Name]; imported {
				if p := a.pkgs[path]; p != nil {
					if f := p.funcs[e.Sel.Name]; f != nil {
						n.calls = append(n.calls, f)
					} else if l := p.lits[e.Sel.Name]; l != nil {
						n.calls = append(n.calls, l)
					}
				}
				return
			}
		}
		n.calls = append(n.calls, a.methods[e.Sel.Name]...)
	}
}

// webCall records a call of the web package's: a read of the environment,
// or a handler of its changes.
func (a *restartAnalysis) webCall(n *restartNode, c *ast.CallExpr, name string) {
	switch name {
	case "Env", "MustEnv":
		if len(c.Args) > 0 {
			n.read(a, c.Args[0], c.Pos(), false)
		}
	case "Require":
		for _, arg := range c.Args {
			n.read(a, arg, c.Pos(), true)
		}
	case "Hot", "OnEnvChange":
		if len(c.Args) > 0 {
			if k, ok := a.keyOf(c.Args[0], n); ok {
				a.hot[k] = true
			}
		}
	}
}

func (n *restartNode) read(a *restartAnalysis, key ast.Expr, pos token.Pos, always bool) {
	k, ok := a.keyOf(key, n)
	n.reads = append(n.reads, restartRead{key: k, literal: ok, always: always, pos: pos})
}
