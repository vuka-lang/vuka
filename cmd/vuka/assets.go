package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// A page's styles and scripts, for apps on github.com/vuka-lang/web/v2:
// web.Style, Script, CSS and JS on @web.Live, web.Layout, web.LiveLayout
// and web.New name them, read here from the source (string literals). Each
// component they belong to — the live component's type, or the layout
// function — gets a scope id; the transpiler marks the elements its markup
// writes with it (transpile.Options.Scopes), its stylesheets are scoped to
// it and compiled with the app's Tailwind CSS (tailwind.go), and its
// scripts are bundled into one module (scripts.go).

const scopeAttr = "data-vk-s"

// pageAssets is what a module declares.
type pageAssets struct {
	root, modPath string
	comps         []*assetComp  // scoped, in the order first declared
	global        *assetComp    // web.New's and web.Global's
	islands       []*islandComp // @ui.Island types, in the order first declared
	frontendRoot  string        // the island build's root (web.Frontend's folder); absolute, "" = module root
}

// assetComp is a component's styles and scripts.
type assetComp struct {
	dir, importPath, name string // its package, and its declaration
	id                    string
	templ                 string   // the .templ file declaring it, for a templ component
	markup                []string // the .vuka.html files holding its templates
	live                  bool     // a live component (@web.Live, a live shape), whose methods its scripts may push
	page                  bool     // a page registered by @web.Live
	styles, scripts       []assetSrc
	// marks are other components of the module whose markup carries the
	// scope, by directory: a library's whose script it is (libraryScripts).
	marks map[string][]string
	// classic: its scripts run as the page's own <script> tags would, a
	// library's (classicBundle).
	classic bool
}

// assetSrc is a stylesheet or script: a file, or inline source.
type assetSrc struct {
	path   string // the file; "" for inline source
	inline string
	label  string // as messages name it: counter.css, or counter.vuka:12
	line   int    // inline source: its line in label's file, less one
	dir    string // where its relative imports start
}

// goName is the component's name in messages: main.Counter.
func (c *assetComp) goName() string {
	if c.name == "" {
		return "the app"
	}
	return transpile.ImportName(c.importPath) + "." + c.name
}

// scopeID is the scope of a package's declaration: short, and the same for
// every build.
func scopeID(importPath, name string) string {
	sum := sha256.Sum256([]byte(importPath + "." + name))
	return hex.EncodeToString(sum[:3])
}

// scopes is transpile's Options.Scopes for each package directory.
func (pa *pageAssets) scopes() map[string]map[string]string {
	if pa == nil || len(pa.comps) == 0 {
		return nil
	}
	out := map[string]map[string]string{}
	for _, c := range pa.comps {
		if out[c.dir] == nil {
			out[c.dir] = map[string]string{}
		}
		out[c.dir][c.name] = c.id
		for dir, names := range c.marks {
			if out[dir] == nil {
				out[dir] = map[string]string{}
			}
			for _, n := range names {
				out[dir][n] = c.id
			}
		}
	}
	return out
}

func (pa *pageAssets) empty() bool {
	return pa == nil || len(pa.comps) == 0 && len(pa.global.styles) == 0 && len(pa.global.scripts) == 0 && len(pa.islands) == 0
}

var assetFuncs = map[string]bool{"Style": true, "Script": true, "CSS": true, "JS": true}

// findAssets reads the asset options of the module at root. Nil when it
// declares none.
func findAssets(root, modPath string) (*pageAssets, error) {
	if mod, err := load.ReadSource(filepath.Join(root, "go.mod")); err != nil || !twRequire.Match(mod) {
		return nil, nil
	}
	pa := &pageAssets{root: root, modPath: modPath, global: &assetComp{}}
	g := &goPkgs{root: root, modPath: modPath, fset: token.NewFileSet(), dirs: map[string][]*goFile{}}
	var errs []error
	err := walkSources(root, func(p, name string) error {
		ext := filepath.Ext(name)
		if strings.HasSuffix(name, ".vuka.html") {
			src, err := load.ReadSource(p)
			if err != nil {
				return err
			}
			pa.readTemplates(p, src)
			return nil
		}
		if ext != ".vuka" && ext != ".go" || strings.HasSuffix(name, "_test"+ext) || strings.HasSuffix(name, "_gen.go") {
			return nil
		}
		src, err := load.ReadSource(p)
		if err != nil {
			return err
		}
		s := string(src)
		if ext == ".vuka" {
			pa.readJSXAssets(p, src)
		}
		if strings.Contains(s, `"`+webPath+`"`) {
			errs = append(errs, pa.readFile(p, src)...)
		}
		if strings.Contains(s, `"`+load.UIPath+`"`) {
			errs = append(errs, pa.readIslands(p, src)...)
			if ext == ".go" && !strings.HasSuffix(name, "_vuka.go") {
				errs = append(errs, pa.readGoIslands(g, p)...)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	pa.islands = dedupeIslands(pa.islands)
	if err := pa.libraryScripts(); err != nil {
		return nil, err
	}
	if pa.empty() {
		return nil, nil
	}
	ids := map[string]*assetComp{}
	for _, c := range pa.comps {
		if o := ids[c.id]; o != nil {
			return nil, diag.New("E0606", "%s and %s share the scope %s", o.goName(), c.goName(), c.id).WithHelp("rename one")
		}
		ids[c.id] = c
	}
	seen := map[string]*islandComp{}
	for _, ic := range pa.islands {
		if o := seen[ic.name]; o != nil {
			return nil, diag.New("E0312", "island %s is already declared at %s", ic.name, o.label).At(labelSpan(ic.label)).
				WithRelated(diag.Span{File: labelSpan(o.label).File, Line: labelSpan(o.label).Line, Label: "declared here first"}).
				WithHelp("two @ui.Island types can't share a name: rename one")
		}
		seen[ic.name] = ic
	}
	if len(pa.islands) > 0 {
		pa.frontendRoot = findFrontendRoot(root)
		for _, ic := range pa.islands {
			ic.locate(root, pa.frontendRoot)
		}
	}
	return pa, nil
}

// wtok is a token of a source file.
type wtok struct {
	off int
	tok token.Token
	lit string
}

// wcall is a call of one of web's functions: web.Style(…).
type wcall struct {
	fn     string
	at     int      // the token of web's name
	rp     int      // the token of its )
	args   [][2]int // each argument's tokens, [start, end)
	parent *wcall
	arg    int // the argument of parent it is
}

func (pa *pageAssets) readFile(file string, src []byte) []error {
	fset := token.NewFileSet()
	tf := fset.AddFile(file, -1, len(src))
	var s scanner.Scanner
	s.Init(tf, src, func(token.Position, string) {}, 0)
	var toks []wtok
	for {
		pos, t, lit := s.Scan()
		if t == token.EOF {
			break
		}
		if t == token.SEMICOLON && lit == "\n" {
			continue
		}
		toks = append(toks, wtok{tf.Offset(pos), t, lit})
	}
	imports := fileImports(toks)
	webName := ""
	for name, p := range imports {
		if p == webPath {
			webName = name
		}
	}
	if webName == "" {
		return nil
	}
	line := func(i int) int { return tf.Line(tf.Pos(toks[i].off)) }
	rel, _ := filepath.Rel(pa.root, file)
	where := func(i int) string { return fmt.Sprintf("%s:%d", rel, line(i)) }

	var calls []*wcall
	for i := 0; i+3 < len(toks); i++ {
		if toks[i].tok != token.IDENT || toks[i].lit != webName || toks[i+1].tok != token.PERIOD ||
			toks[i+2].tok != token.IDENT || toks[i+3].tok != token.LPAREN {
			continue
		}
		c := &wcall{fn: toks[i+2].lit, at: i}
		depth, start := 0, i+4
		for j := i + 3; j < len(toks); j++ {
			switch toks[j].tok {
			case token.LPAREN, token.LBRACK, token.LBRACE:
				depth++
			case token.RPAREN, token.RBRACK, token.RBRACE:
				depth--
				if depth == 0 {
					if j > start {
						c.args = append(c.args, [2]int{start, j})
					}
					c.rp = j
				}
			case token.COMMA:
				if depth == 1 {
					c.args = append(c.args, [2]int{start, j})
					start = j + 1
				}
			}
			if c.rp > 0 {
				break
			}
		}
		if c.rp > 0 {
			calls = append(calls, c)
		}
	}
	for _, c := range calls {
		for _, p := range calls {
			for k, a := range p.args {
				if a[0] == c.at && a[1] == c.rp+1 {
					c.parent, c.arg = p, k
				}
			}
		}
	}

	var errs []error
	fail := func(i int, format string, args ...any) {
		errs = append(errs, diag.New("E0604", format, args...).At(labelSpan(where(i))))
	}
	dir := filepath.Dir(file)
	for _, c := range calls {
		if !assetFuncs[c.fn] {
			continue
		}
		label := webName + "." + c.fn
		var vals []string
		var valToks []int
		global, ok := false, true
		for _, a := range c.args {
			switch {
			case a[1]-a[0] == 1 && toks[a[0]].tok == token.STRING:
				v, err := strconv.Unquote(toks[a[0]].lit)
				if err != nil {
					ok = false
					continue
				}
				if v == ":global" {
					global = true
					continue
				}
				vals, valToks = append(vals, v), append(valToks, a[0])
			case a[1]-a[0] == 3 && toks[a[0]].lit == webName && toks[a[0]+2].lit == "Global":
				global = true
			default:
				fail(a[0], "%s takes string literals (the vuka command reads them from the source)", label)
				ok = false
			}
		}
		if !ok {
			continue
		}
		owner := c.parent
		for owner != nil && owner.fn == "Options" {
			owner = owner.parent
		}
		var comp *assetComp
		switch {
		case owner == nil:
			fail(c.at, "%s belongs to no page: give it to @%s.Live, %s.Layout, %s.LiveLayout or %s.New", label, webName, webName, webName, webName)
			continue
		case global || owner.fn == "New":
			comp = pa.global
		case owner.fn == "Live" && toks[owner.at].off > 0 && src[toks[owner.at].off-1] == '@':
			name := decoratedType(toks, owner.rp+1)
			if name == "" {
				fail(owner.at, "@%s.Live isn't on a type", webName)
				continue
			}
			comp = pa.comp(dir, pa.importPathOf(dir), name, "")
			comp.live, comp.page = true, true
		case (owner.fn == "Layout" || owner.fn == "LiveLayout") && c.arg > 0 && len(owner.args) > 0:
			var err string
			comp, err = pa.layoutComp(dir, imports, toks, owner.args[0])
			if err != "" {
				fail(owner.args[0][0], "%s: %s", label, err)
				continue
			}
		default:
			fail(c.at, "%s belongs to no page: give it to @%s.Live, %s.Layout, %s.LiveLayout or %s.New", label, webName, webName, webName, webName)
			continue
		}
		for k, v := range vals {
			ln := line(valToks[k])
			switch c.fn {
			case "CSS", "JS":
				s := assetSrc{inline: v, label: fmt.Sprintf("%s:%d", rel, ln), line: ln - 1, dir: dir}
				if c.fn == "CSS" {
					comp.styles = append(comp.styles, s)
				} else {
					comp.scripts = append(comp.scripts, s)
				}
			default:
				exts := []string{".css"}
				if c.fn == "Script" {
					exts = []string{".ts", ".js", ".mts", ".mjs"}
				}
				files, err := expandAssets(dir, v, exts)
				if err != nil {
					d := diag.New("E0605", "%s(%q): %v", label, v, err).At(diag.Span{File: rel, Line: ln})
					var nf *diag.Diagnostic
					if errors.As(err, &nf) {
						d.Message, d.Suggest = fmt.Sprintf("%s(%q): %s", label, v, nf.Message), nf.Suggest
					}
					errs = append(errs, d)
					continue
				}
				for _, f := range files {
					r, _ := filepath.Rel(pa.root, f)
					s := assetSrc{path: f, label: filepath.ToSlash(r), dir: filepath.Dir(f)}
					list := &comp.styles
					if c.fn == "Script" {
						list = &comp.scripts
					}
					if !slices.ContainsFunc(*list, func(x assetSrc) bool { return x.path == f }) {
						*list = append(*list, s)
					}
				}
			}
		}
	}
	return errs
}

// comp is the component of a declaration, made once.
func (pa *pageAssets) comp(dir, importPath, name, templ string) *assetComp {
	for _, c := range pa.comps {
		if c.dir == dir && c.name == name {
			return c
		}
	}
	c := &assetComp{dir: dir, importPath: importPath, name: name, id: scopeID(importPath, name), templ: templ}
	pa.comps = append(pa.comps, c)
	return c
}

// readTemplates reads the scripts and stylesheets of a template file's
// templates: each its template's, a stylesheet without scoped the app's.
func (pa *pageAssets) readTemplates(file string, src []byte) {
	src = transpile.HeaderView(src)
	dir := filepath.Dir(file)
	rel, _ := filepath.Rel(pa.root, file)
	// The component files its imports name are islands.
	top := 0
	for _, l := range strings.SplitAfter(string(src), "\n")[:transpile.ParseTemplateHead(src).Top] {
		top += len(l)
	}
	full, line := scanFile(file, append(src[:top:top], "\nfunc"...)) // the imports end at a declaration
	pa.readIslandImports(file, filepath.ToSlash(rel), line, dropNewlineSemis(full))
	shape, _ := transpile.ParseShape(src)
	for _, a := range transpile.TemplateAssets(filepath.Base(file), src) {
		s := assetSrc{inline: a.Text, label: fmt.Sprintf("%s:%d", rel, a.Line), line: a.Line - 1, dir: dir}
		switch {
		case !a.Script && !a.Scoped:
			pa.global.styles = append(pa.global.styles, s)
		case a.Script:
			c := pa.comp(dir, pa.importPathOf(dir), a.Decl, "")
			c.scripts = append(c.scripts, s)
			// A live shape's script pushes its methods (live.reorder).
			if shape != nil && shape.Type == a.Decl && shape.Live {
				c.live = true
			}
			c.markup = append(c.markup, file)
		default:
			c := pa.comp(dir, pa.importPathOf(dir), a.Decl, "")
			c.styles = append(c.styles, s)
			c.markup = append(c.markup, file)
		}
	}
}

var (
	jsxAsset = regexp.MustCompile(`(?s)<(script)\s+lang="ts"\s*>(.*?)</script>|<(style)\s+scoped\s*>(.*?)</style>`)
	jsxDecl  = regexp.MustCompile(`(?m)^func\s*(?:\(\s*\w*\s*\*?\s*(\w+)(?:\[[^\]]*\])?\s*\)\s*\w+|(\w+))`)
)

// readJSXAssets reads the component scripts and scoped stylesheets a .vuka
// file's JSX holds, <script lang="ts"> and <style scoped>: each the
// component's whose function or method it is in.
func (pa *pageAssets) readJSXAssets(file string, src []byte) {
	dir := filepath.Dir(file)
	rel, _ := filepath.Rel(pa.root, file)
	for _, m := range jsxAsset.FindAllSubmatchIndex(src, -1) {
		decl := ""
		for _, d := range jsxDecl.FindAllSubmatch(src[:m[0]], -1) {
			decl = string(d[1]) + string(d[2])
		}
		if decl == "" {
			continue
		}
		body, at := m[4], m[5]
		if m[2] < 0 {
			body, at = m[8], m[9]
		}
		text := string(src[body:at])
		line := 1 + bytes.Count(src[:body], []byte("\n"))
		if strings.HasPrefix(text, "\n") {
			text, line = text[1:], line+1
		}
		s := assetSrc{inline: text, label: fmt.Sprintf("%s:%d", rel, line), line: line - 1, dir: dir}
		c := pa.comp(dir, pa.importPathOf(dir), decl, "")
		if m[2] >= 0 {
			c.scripts = append(c.scripts, s)
		} else {
			c.styles = append(c.styles, s)
		}
	}
}

func (pa *pageAssets) importPathOf(dir string) string {
	rel, _ := filepath.Rel(pa.root, dir)
	if rel == "." {
		return pa.modPath
	}
	return pa.modPath + "/" + filepath.ToSlash(rel)
}

// layoutComp is the component of a layout, the function a web.Layout's
// first argument names: a function of a .vuka file, or a templ component,
// in this package or another of the module.
func (pa *pageAssets) layoutComp(dir string, imports map[string]string, toks []wtok, arg [2]int) (*assetComp, string) {
	name, pkgDir := "", dir
	switch n := arg[1] - arg[0]; {
	case n == 1 && toks[arg[0]].tok == token.IDENT:
		name = toks[arg[0]].lit
	case n == 3 && toks[arg[0]].tok == token.IDENT && toks[arg[0]+1].tok == token.PERIOD && toks[arg[0]+2].tok == token.IDENT:
		p, ok := imports[toks[arg[0]].lit]
		rel, in := strings.CutPrefix(p, pa.modPath+"/")
		if !ok || !in {
			return nil, fmt.Sprintf("%s isn't a package of this module; only a layout of the module can have styles and scripts", toks[arg[0]].lit)
		}
		name, pkgDir = toks[arg[0]+2].lit, filepath.Join(pa.root, filepath.FromSlash(rel))
	default:
		return nil, "the layout must be a named function (counterPage, views.Shell): styles and scripts belong to its markup"
	}
	entries, _ := load.ReadDir(pkgDir)
	funcDecl := regexp.MustCompile(`(?m)^func\s+` + regexp.QuoteMeta(name) + `\s*[\[(]`)
	for _, e := range entries {
		src, err := load.ReadSource(filepath.Join(pkgDir, e.Name()))
		switch {
		case err != nil:
		case strings.HasSuffix(e.Name(), ".templ") && declaresTempl(src, name):
			return pa.comp(pkgDir, pa.importPathOf(pkgDir), name, filepath.Join(pkgDir, e.Name())), ""
		case strings.HasSuffix(e.Name(), ".vuka") && funcDecl.Match(src):
			return pa.comp(pkgDir, pa.importPathOf(pkgDir), name, ""), ""
		}
	}
	return nil, fmt.Sprintf("%s isn't a function of a .vuka file or a templ component of %s: only their markup can carry its scope", name, pa.importPathOf(pkgDir))
}

var templDeclRe = regexp.MustCompile(`(?m)^templ\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)\s*[\[(]`)

func declaresTempl(src []byte, name string) bool {
	for _, m := range templDeclRe.FindAllSubmatch(src, -1) {
		if string(m[1]) == name {
			return true
		}
	}
	return false
}

// decoratedType is the type the decorators from toks[i] are on: other
// decorators and attributes may come between.
func decoratedType(toks []wtok, i int) string {
	for i < len(toks) {
		t := toks[i]
		switch {
		case t.tok == token.ILLEGAL && t.lit == "@":
			i++
			for i < len(toks) && (toks[i].tok == token.IDENT || toks[i].tok == token.PERIOD) {
				i++
			}
			if i < len(toks) && (toks[i].tok == token.LPAREN || toks[i].tok == token.LBRACE) {
				depth := 0
				for ; i < len(toks); i++ {
					switch toks[i].tok {
					case token.LPAREN, token.LBRACE, token.LBRACK:
						depth++
					case token.RPAREN, token.RBRACE, token.RBRACK:
						depth--
					}
					if depth == 0 {
						i++
						break
					}
				}
			}
		case t.tok == token.TYPE && i+1 < len(toks) && toks[i+1].tok == token.IDENT:
			return toks[i+1].lit
		default:
			return ""
		}
	}
	return ""
}

// fileImports maps each import's name in a file to its path.
func fileImports(toks []wtok) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(toks); i++ {
		switch toks[i].tok {
		case token.FUNC, token.TYPE, token.VAR, token.CONST:
			return out
		case token.IMPORT:
		default:
			continue
		}
		spec := func(j int) int {
			name := ""
			if j < len(toks) && (toks[j].tok == token.IDENT || toks[j].tok == token.PERIOD) {
				name = toks[j].lit
				j++
			}
			if j < len(toks) && toks[j].tok == token.STRING {
				if p, err := strconv.Unquote(toks[j].lit); err == nil {
					if name == "" {
						name = transpile.ImportName(p)
					}
					out[name] = p
				}
			}
			return j
		}
		i++
		if i < len(toks) && toks[i].tok == token.LPAREN {
			for i++; i < len(toks) && toks[i].tok != token.RPAREN; i++ {
				if toks[i].tok != token.SEMICOLON {
					i = spec(i)
				}
			}
		} else {
			i = spec(i)
		}
	}
	return out
}

// expandAssets is the files pattern names, relative to dir: a file, or a
// glob (* and ? within a name, ** across directories), in name order. A
// pattern naming nothing is an error, with the nearest file as a
// suggestion.
func expandAssets(dir, pattern string, exts []string) ([]string, error) {
	pattern = filepath.ToSlash(pattern)
	if !strings.ContainsAny(pattern, "*?[") {
		p := filepath.Join(dir, filepath.FromSlash(pattern))
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			if !slices.Contains(exts, filepath.Ext(p)) {
				return nil, fmt.Errorf("not a %s file", strings.Join(exts, ", "))
			}
			return []string{p}, nil
		}
		return nil, notFound(dir, pattern, exts, "no such file")
	}
	segs := strings.Split(path.Clean(pattern), "/")
	base := dir
	for len(segs) > 1 && !strings.ContainsAny(segs[0], "*?[") {
		base, segs = filepath.Join(base, segs[0]), segs[1:]
	}
	var out []string
	load.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() && p != base && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(base, p)
		if globMatch(segs, strings.Split(filepath.ToSlash(rel), "/")) && slices.Contains(exts, filepath.Ext(p)) {
			out = append(out, p)
		}
		return nil
	})
	if len(out) == 0 {
		return nil, notFound(dir, pattern, exts, "matches no file")
	}
	slices.Sort(out)
	return out, nil
}

// globMatch matches path segments against a pattern's, ** matching any
// number of directories.
func globMatch(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for k := 0; k <= len(segs); k++ {
			if globMatch(pat[1:], segs[k:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	ok, _ := path.Match(pat[0], segs[0])
	return ok && globMatch(pat[1:], segs[1:])
}

// notFound says a pattern names nothing, suggesting the nearest file of
// its kind in the directory it names.
func notFound(dir, pattern string, exts []string, what string) error {
	sub := filepath.Join(dir, filepath.FromSlash(path.Dir(pattern)))
	entries, _ := load.ReadDir(sub)
	var names []string
	for _, e := range entries {
		if !e.IsDir() && slices.Contains(exts, filepath.Ext(e.Name())) {
			names = append(names, path.Join(path.Dir(pattern), e.Name()))
		}
	}
	if s := closest(pattern, names); s != "" {
		return diag.New("E0605", "%s", what).WithSuggest(s)
	}
	return diag.New("E0605", "%s", what)
}

// closest is the name nearest to s by edit distance, when one is near.
func closest(s string, names []string) string {
	best, bestD := "", max(3, len(s)/3+1)
	for _, n := range names {
		if d := editDistance(strings.ToLower(s), strings.ToLower(n)); d < bestD && d < len(n) {
			best, bestD = n, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// loadOptions is load.Options carrying the module's scopes.
func loadOptions(pa *pageAssets) load.Options { return load.Options{Scopes: pa.scopes()} }
