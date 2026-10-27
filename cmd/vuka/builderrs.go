package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/docs"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// A failed build says what is wrong once: the errors of the packages that
// fail themselves, each at the line and column of the source it was written
// in, and in one line how many files fail only because they import one of
// them. vuka build, run, run -watch, vet and the toolbar show this report.

// buildReport is what a failed build says.
type buildReport struct {
	groups    []errGroup        // the packages' errors, lower packages first
	importers []diag.Diagnostic // errors of files that only import a failing package
	raw       []string          // what the go command said that isn't a package's errors
}

// errGroup is the errors of one package.
type errGroup struct {
	path  string
	diags []diag.Diagnostic
}

// maxErrors is how many errors of a package vuka build and run show; vuka
// vet shows them all.
const maxErrors = 10

// reportOptions say how a report is written.
type reportOptions struct {
	color   bool
	verbose bool // list the files that fail only because of an import
	all     bool // every error, not maxErrors a package
}

// errs is how many errors the report has.
func (r *buildReport) errs() int {
	n := 0
	for _, g := range r.groups {
		n += len(g.diags)
	}
	return n
}

func (r *buildReport) empty() bool { return len(r.groups) == 0 && len(r.raw) == 0 }

// write writes the report: each package's errors, what else the go command
// said, then a line of totals.
func (r *buildReport) write(w io.Writer, o reportOptions) {
	var b bytes.Buffer
	wd := workDir()
	ro := diag.Options{Color: o.color, Root: wd, NoFooter: true}
	var shown []diag.Diagnostic
	for _, g := range r.groups {
		ds := g.diags
		if !o.all && len(ds) > maxErrors {
			ds = ds[:maxErrors]
		}
		ds = fillHelp(relDiags(slices.Clone(ds), wd))
		if len(shown) > 0 {
			b.WriteByte('\n')
		}
		diag.Render(&b, ds, ro)
		if n := len(g.diags) - len(ds); n > 0 {
			fmt.Fprintf(&b, "\nand %d more %s in %s — `vuka vet` lists all\n", n, countWord(n, "error", "errors"), g.path)
		}
		shown = append(shown, ds...)
	}
	for _, l := range r.raw {
		b.WriteString(l)
		if !strings.HasSuffix(l, "\n") {
			b.WriteByte('\n')
		}
	}
	// Go's own type errors need no explaining; what vuka's codes mean does.
	diag.Footer(&b, slices.DeleteFunc(slices.Clone(shown), func(d diag.Diagnostic) bool { return d.Code == "E0002" }), ro)
	if o.verbose && len(r.importers) > 0 {
		fmt.Fprintf(&b, "\nthe %s that couldn't build because of %s:\n", importerNoun(r.importers, wd, len(importerFiles(r.importers)) != 1), countWord(r.errs(), "it", "them"))
		for _, d := range relDiags(slices.Clone(r.importers), wd) {
			fmt.Fprintf(&b, "  %s: imports %s\n", d.Span, culpritOf(d.Message))
		}
	}
	if line := r.summary(wd); line != "" {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line + "\n")
	}
	w.Write(b.Bytes())
}

// fillHelp gives the diagnostics their codes' help, but Go's own type
// errors, whose message says it.
func fillHelp(ds []diag.Diagnostic) []diag.Diagnostic {
	for i := range ds {
		if ds[i].Code != "E0002" {
			ds[i] = docs.Fill(ds[i : i+1])[0]
		}
	}
	return ds
}

// summary is the report's line of totals: "2 errors in 2 packages · 11
// pages couldn't build because of them".
func (r *buildReport) summary(wd string) string {
	n := r.errs()
	if n == 0 {
		return ""
	}
	s := fmt.Sprintf("%d %s in %d %s", n, countWord(n, "error", "errors"), len(r.groups), countWord(len(r.groups), "package", "packages"))
	if files := len(importerFiles(r.importers)); files > 0 {
		s += fmt.Sprintf(" · %d %s couldn't build because of %s", files, importerNoun(r.importers, wd, files != 1), countWord(n, "it", "them"))
	}
	return s
}

func countWord(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// importerFiles are the files of the importers' errors.
func importerFiles(ds []diag.Diagnostic) []string {
	var files []string
	for _, d := range ds {
		if !slices.Contains(files, d.Span.File) {
			files = append(files, d.Span.File)
		}
	}
	return files
}

// importerNoun is what the importers are: pages when they are all under
// pages/, else files.
func importerNoun(ds []diag.Diagnostic, wd string, many bool) string {
	pages := len(ds) > 0
	for _, d := range ds {
		f := filepath.ToSlash(d.Span.File)
		if filepath.IsAbs(d.Span.File) {
			if root, _, err := load.ModuleRoot(wd); err == nil {
				if rel, err := filepath.Rel(root, d.Span.File); err == nil {
					f = filepath.ToSlash(rel)
				}
			}
		}
		pages = pages && strings.HasPrefix(f, "pages/")
	}
	switch {
	case pages && many:
		return "pages"
	case pages:
		return "page"
	case many:
		return "files"
	}
	return "file"
}

func workDir() string {
	wd, _ := os.Getwd()
	if r, err := filepath.EvalSymlinks(wd); err == nil {
		wd = r
	}
	return wd
}

// failingImport matches the message of an error at an import of a package
// that doesn't compile, naming the package at fault.
var failingImport = []*regexp.Regexp{
	regexp.MustCompile(`^\S+ depends on (\S+), which doesn't compile`),
	regexp.MustCompile(`^(\S+) doesn't compile\b`),
	regexp.MustCompile(`^(\S+) has errors$`),
}

// culpritOf is the package an import error's message names as failing, ""
// when it names none.
func culpritOf(msg string) string {
	for _, re := range failingImport {
		if m := re.FindStringSubmatch(msg); m != nil {
			return m[1]
		}
	}
	return ""
}

// buildInput is what a build had when it failed.
type buildInput struct {
	root, modPath string
	overlay       string
	gens          []load.Generated
}

// transpileReport is the report of a transpile that failed with err; nil
// when err holds no diagnostics.
func transpileReport(in buildInput, err error) *buildReport {
	ds, rest := diag.From(err)
	if len(ds) == 0 {
		return nil
	}
	r := &buildReport{}
	for _, e := range rest {
		r.raw = append(r.raw, e.Error())
	}
	// The packages the errors at imports blame, checked for their own
	// errors at their source.
	var culprits []string
	for _, d := range ds {
		if c := culpritOf(d.Message); d.Code == "E0005" && c != "" && in.inModule(c) && !slices.Contains(culprits, c) && !strings.HasSuffix(d.Message, " has errors") {
			culprits = append(culprits, c)
		}
	}
	own := in.check(culprits)
	byPkg := map[string][]diag.Diagnostic{}
	var order []string
	add := func(path string, d diag.Diagnostic) {
		if _, ok := byPkg[path]; !ok {
			order = append(order, path)
		}
		byPkg[path] = append(byPkg[path], d)
	}
	// A package failing only for its imports that don't compile (their
	// types invalid in it) is counted with the files importing it, once
	// what it imports is reported.
	onlyImports := map[string][]diag.Diagnostic{}
	for _, c := range culprits {
		if len(own[c]) > 0 && !slices.ContainsFunc(own[c], func(d diag.Diagnostic) bool { return d.Code != "E0005" }) {
			onlyImports[c] = own[c]
			continue
		}
		for _, d := range own[c] {
			add(c, d)
		}
	}
	for _, d := range ds {
		if d.Code != "E0005" || d.Severity == diag.Warning {
			add(in.pkgOf(d.Span.File), d)
		}
	}
	reported := map[string]bool{} // the packages whose own errors are shown
	for path := range byPkg {
		reported[path] = true
	}
	for _, c := range culprits {
		ds := onlyImports[c]
		if len(ds) == 0 {
			continue
		}
		if slices.ContainsFunc(ds, func(d diag.Diagnostic) bool { return reported[culpritOf(d.Message)] }) {
			r.importers = append(r.importers, ds...)
			reported[c] = true
			continue
		}
		for _, d := range ds {
			add(c, d)
		}
		reported[c] = true
	}
	// A package whose errors are all at imports of reported packages is
	// reported through them: an import of it is one more importer, all the
	// way up the chain (a Go package importing a broken Vuka package, the
	// Vuka packages importing that Go package, the pages importing them).
	through := map[string][]string{} // package → the culprits of its import errors
	alone := map[string]bool{}       // packages with an import error naming no culprit
	for _, d := range ds {
		if d.Code != "E0005" || d.Severity == diag.Warning {
			continue
		}
		p := in.pkgOf(d.Span.File)
		if c := culpritOf(d.Message); c != "" {
			through[p] = append(through[p], c)
		} else {
			alone[p] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for p, cs := range through {
			if !reported[p] && !alone[p] && !slices.ContainsFunc(cs, func(c string) bool { return !reported[c] }) {
				reported[p], changed = true, true
			}
		}
	}
	kept := map[string]bool{}
	for _, d := range ds {
		if d.Code != "E0005" || d.Severity == diag.Warning {
			continue
		}
		c := culpritOf(d.Message)
		switch {
		case c != "" && reported[c]:
			r.importers = append(r.importers, d)
		case c != "" && kept[c]:
			r.importers = append(r.importers, d) // said once already
		default:
			kept[c] = true
			add(in.pkgOf(d.Span.File), d)
		}
	}
	for _, path := range in.depOrder(order) {
		r.groups = append(r.groups, errGroup{path: path, diags: byPkg[path]})
	}
	r.stoppedBefore()
	return r
}

// stoppedBefore says, of a model's typed fields a failed transpile left
// ungenerated, why: the build stopped at another package's error before
// the orm wrote them.
func (r *buildReport) stoppedBefore() {
	const missed = " wasn't generated for this build"
	var cause *diag.Diagnostic
	for i := range r.groups {
		for j := range r.groups[i].diags {
			if d := &r.groups[i].diags[j]; cause == nil && !strings.Contains(d.Message, missed) && d.Code != "E0005" {
				cause = d
			}
		}
	}
	if cause == nil {
		return
	}
	at := fmt.Sprintf("%s:%d", filepath.ToSlash(cause.Span.File), cause.Span.Line)
	if rel, err := filepath.Rel(workDir(), cause.Span.File); err == nil && filepath.IsAbs(cause.Span.File) && !strings.HasPrefix(rel, "..") {
		at = fmt.Sprintf("%s:%d", filepath.ToSlash(rel), cause.Span.Line)
	}
	for i := range r.groups {
		for j := range r.groups[i].diags {
			d := &r.groups[i].diags[j]
			if strings.Contains(d.Message, missed) {
				d.Message = strings.Replace(d.Message, missed, " wasn't generated: the build stopped at the error at "+at+" before the orm wrote it", 1)
				d.Help = ""
			}
		}
	}
}

// inModule reports whether path is a package of the main module.
func (in buildInput) inModule(path string) bool {
	return in.modPath != "" && (path == in.modPath || strings.HasPrefix(path, in.modPath+"/"))
}

// dirOf is the directory the go command sees the module's package path
// in, by its real path as the overlay names files.
func (in buildInput) dirOf(path string) string {
	root := realPathOf(in.root)
	if path == in.modPath {
		return root
	}
	return filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(path, in.modPath+"/")))
}

// pkgOf is the import path of the module's package file is in.
func (in buildInput) pkgOf(file string) string {
	if file == "" {
		return ""
	}
	dir := load.GoDir(in.root, filepath.Dir(file))
	rel, err := filepath.Rel(realPathOf(in.root), realPathOf(dir))
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	if rel == "." {
		return in.modPath
	}
	return in.modPath + "/" + filepath.ToSlash(rel)
}

// depOrder is paths with each after the module's packages it imports.
func (in buildInput) depOrder(paths []string) []string {
	if len(paths) < 2 {
		return paths
	}
	ov, _ := readOverlay(in.overlay)
	ctx := overlayContext(ov)
	want := map[string]bool{}
	for _, p := range paths {
		want[p] = true
	}
	var out []string
	seen := map[string]bool{}
	var visit func(p string)
	visit = func(p string) {
		if seen[p] {
			return
		}
		seen[p] = true
		if p != "" && in.inModule(p) {
			if bp, _ := ctx.ImportDir(in.dirOf(p), 0); bp != nil {
				for _, imp := range bp.Imports {
					if in.inModule(imp) {
						visit(imp)
					}
				}
			}
		}
		if want[p] {
			out = append(out, p)
		}
	}
	for _, p := range paths {
		visit(p)
	}
	return out
}

// goReport is the report of a go command that failed, from what it said:
// each package's compile errors checked again at their source; what else
// it said as it is.
func goReport(in buildInput, out []byte) *buildReport {
	var rest bytes.Buffer
	b := &blockWriter{next: &rest, respell: func(l string) string { return l }}
	b.Write(out)
	b.flush()
	lines := strings.SplitAfter(rest.String(), "\n")
	return blocksReport(in, b.blocks, slices.DeleteFunc(lines, func(l string) bool { return l == "" }))
}

func blocksReport(in buildInput, blocks []goBlock, rest []string) *buildReport {
	r := &buildReport{raw: rest}
	var paths []string
	for _, b := range blocks {
		if in.inModule(b.path) && !slices.Contains(paths, b.path) {
			paths = append(paths, b.path)
		}
	}
	own := in.check(paths)
	var order []string
	for _, b := range blocks {
		if len(own[b.path]) > 0 {
			if !slices.Contains(order, b.path) {
				order = append(order, b.path)
			}
			continue
		}
		r.raw = append(r.raw, b.lines...)
	}
	for _, path := range in.depOrder(order) {
		r.groups = append(r.groups, errGroup{path: path, diags: own[path]})
	}
	return r
}

// goBlock is the errors the go command printed for a package: "# path"
// and the lines below it.
type goBlock struct {
	path   string
	lines  []string // with the "# path" line, each ending in a newline
	closed bool     // a line not of its errors came after it
}

var (
	blockHead = regexp.MustCompile(`^# \[?([^\s\[\]]+)\]?( \[\S+\])?\r?\n?$`)
	blockLine = regexp.MustCompile(`^(vet: )?\S+?:\d+(:\d+)?: |^\t`)
)

// check type-checks the module's packages paths as the build's overlay has
// them, and returns their errors at the source each was written in, with
// what vuka can tell about them; a package that checks clean has none.
func (in buildInput) check(paths []string) map[string][]diag.Diagnostic {
	out := map[string][]diag.Diagnostic{}
	if len(paths) == 0 {
		return out
	}
	ov, err := readOverlay(in.overlay)
	if err != nil {
		return out
	}
	c := &checker{in: in, ov: ov, ctx: overlayContext(ov), gens: map[string]*load.Generated{}, knock: map[string][]diag.Diagnostic{}}
	for i := range in.gens {
		c.gens[realPathOf(in.gens[i].Target)] = &in.gens[i]
	}
	c.im = load.NewOverlayImporter(in.root, in.overlay)
	if v, err := load.ModuleGoVersion(in.root); err == nil {
		if parts := strings.SplitN(v, ".", 3); len(parts) >= 2 {
			c.goVersion = "go" + parts[0] + "." + parts[1]
		}
	}
	for _, p := range paths {
		out[p] = c.pkg(p)
		if len(out[p]) == 0 && len(c.knock[p]) > 0 {
			out[p] = c.knock[p] // only its imports: the E0005s say so
		}
	}
	return out
}

type checker struct {
	in        buildInput
	ov        map[string]string
	ctx       *build.Context
	gens      map[string]*load.Generated   // by real target path
	knock     map[string][]diag.Diagnostic // by package: its imports that don't compile
	im        *load.Importer
	goVersion string
}

// checkedFile is a file of a package being checked.
type checkedFile struct {
	path string // where the go command sees it
	ast  *ast.File
	gen  *load.Generated // nil for a file of the module's own
}

// pkg is the errors of the package path.
func (c *checker) pkg(path string) []diag.Diagnostic {
	dir := c.in.dirOf(path)
	// A package the build context finds trouble with (two package names)
	// still lists its files.
	bp, _ := c.ctx.ImportDir(dir, 0)
	if bp == nil || len(bp.GoFiles) == 0 {
		return nil
	}
	fset := token.NewFileSet()
	var files []*checkedFile
	var asts []*ast.File
	var ds []diag.Diagnostic
	for _, name := range bp.GoFiles {
		full := filepath.Join(dir, name)
		cf := &checkedFile{path: full, gen: c.gens[realPathOf(full)]}
		var src []byte
		if cf.gen != nil {
			src = cf.gen.Src
		} else if r, ok := c.ov[full]; ok && r != "" {
			src, _ = os.ReadFile(r)
		} else {
			src, _ = os.ReadFile(full)
		}
		f, err := parser.ParseFile(fset, full, src, parser.ParseComments|parser.SkipObjectResolution)
		var list scanner.ErrorList
		if errors.As(err, &list) && f != nil {
			for _, e := range list {
				p := fset.File(f.Pos()).Pos(e.Pos.Offset)
				ds = append(ds, c.diagnostic(fset, files, cf, p, e.Msg, nil, nil))
			}
		}
		if f == nil {
			continue
		}
		cf.ast = f
		files = append(files, cf)
		asts = append(asts, f)
	}
	if len(ds) > 0 {
		return dedupe(ds)
	}
	var imports []string
	for _, imp := range bp.Imports {
		if imp != "C" && imp != "unsafe" {
			imports = append(imports, imp)
		}
	}
	_ = c.im.Prefetch(imports)
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}, Scopes: map[ast.Node]*types.Scope{}}
	var errs []types.Error
	conf := types.Config{Importer: c.im, GoVersion: c.goVersion, Sizes: types.SizesFor("gc", runtime.GOARCH), FakeImportC: true,
		Error: func(err error) {
			if te, ok := err.(types.Error); ok {
				errs = append(errs, te)
			}
		}}
	pkg, _ := conf.Check(path, fset, asts, info)
	broken := transpile.BrokenImports(errs)
	for _, te := range errs {
		var cf *checkedFile
		for _, f := range files {
			if f.ast.FileStart <= te.Pos && te.Pos <= f.ast.FileEnd {
				cf = f
			}
		}
		if cf == nil {
			continue
		}
		if rest, ok := strings.CutPrefix(te.Msg, "could not import "); ok {
			imp, _, _ := strings.Cut(rest, " ")
			sp := c.toSource(fset, cf, te.Pos, token.NoPos)
			if why := transpile.ImportFailure(te.Msg); culpritOf(why) == "" {
				// No package's errors explain it: the import itself is
				// the error (a path no module has, a folder of the
				// module without the package), at the file importing it.
				help := "fix the import path"
				if strings.Contains(why, "has only .vuka files") {
					help = "build with the vuka command (vuka build, run, test), which generates their Go; for plain go tools, write it beside them with vuka gen -inplace"
				}
				ds = append(ds, diag.Diagnostic{Code: "E0005", Message: why, Help: help, Span: diag.Span{File: sp.file, Line: sp.line, Col: sp.col}})
				continue
			}
			// The package it names has its own errors; what this one
			// can't do with its types is said by them.
			c.knock[path] = append(c.knock[path], diag.Diagnostic{Code: "E0005", Message: imp + " has errors", Span: diag.Span{File: sp.file, Line: sp.line, Col: sp.col}})
			continue
		}
		if transpile.KnockOn(info, cf.ast, te, broken) {
			continue
		}
		ds = append(ds, c.diagnostic(fset, files, cf, te.Pos, te.Msg, info, pkg))
	}
	return dedupe(ds)
}

// dedupe drops the errors said twice at one place (Go written twice for one
// attribute), and sorts them by position.
func dedupe(ds []diag.Diagnostic) []diag.Diagnostic {
	slices.SortStableFunc(ds, func(a, b diag.Diagnostic) int {
		switch {
		case a.Span.File != b.Span.File:
			return strings.Compare(a.Span.File, b.Span.File)
		case a.Span.Line != b.Span.Line:
			return a.Span.Line - b.Span.Line
		}
		return a.Span.Col - b.Span.Col
	})
	return slices.CompactFunc(ds, func(a, b diag.Diagnostic) bool {
		return a.Span.File == b.Span.File && a.Span.Line == b.Span.Line && a.Span.Col == b.Span.Col && a.Message == b.Message
	})
}

// srcPos is a place in the source a file was generated from.
type srcPos struct {
	file      string
	src       []byte // the source's text; nil when unknown
	off, end  int    // byte offsets in src, when src is known
	line, col int
	endCol    int // on the same line; 0 for none
}

// toSource maps the span from pos to end (end may be NoPos) in a checked
// file to the source the user wrote: through the generated file's source
// map, else its line directives, else the file itself.
func (c *checker) toSource(fset *token.FileSet, cf *checkedFile, pos, end token.Pos) srcPos {
	raw := fset.PositionFor(pos, false)
	g := cf.gen
	if g != nil && g.Map != nil && g.Source != "" && g.From != nil {
		if s, _, ok := g.Map.ToSource(raw.Offset); ok {
			sp := srcPos{file: g.Source, src: g.From, off: s, end: s}
			if end.IsValid() {
				ge := fset.PositionFor(end, false).Offset
				e, _, ok := g.Map.ToSource(ge)
				if ge > raw.Offset && (!ok || e < s) {
					if last, _, ok2 := g.Map.ToSource(ge - 1); ok2 && last >= s {
						e, ok = last+1, true
					}
				}
				if ok && e > s {
					sp.end = e
				}
			}
			sp.line, sp.col = lineCol(g.From, s)
			if el, ec := lineCol(g.From, sp.end); el == sp.line && sp.end > s {
				sp.endCol = ec
			}
			return sp
		}
	}
	if g != nil {
		adj := fset.Position(pos)
		file := adj.Filename
		if !filepath.IsAbs(file) {
			file = filepath.Join(filepath.Dir(cf.path), file)
		}
		return srcPos{file: file, line: adj.Line, col: adj.Column, off: -1}
	}
	src, _ := os.ReadFile(cf.path)
	sp := srcPos{file: cf.path, src: src, off: raw.Offset, end: raw.Offset, line: raw.Line, col: raw.Column}
	if end.IsValid() {
		if e := fset.PositionFor(end, false); e.Line == raw.Line && e.Offset > raw.Offset {
			sp.end, sp.endCol = e.Offset, e.Column
		}
	}
	return sp
}

// withArgs extends a span ending at a call's "(" — the Go a template's
// call became maps its function only — over the arguments to the ")".
func (sp *srcPos) withArgs() {
	if sp.src == nil || sp.off < 0 {
		return
	}
	if sp.end <= sp.off {
		// Go vuka wrote maps to where its source starts: the function's name.
		e := sp.off
		for e < len(sp.src) && (isIdentByte(sp.src[e]) || sp.src[e] == '.') {
			e++
		}
		sp.end, sp.endCol = e, sp.col+e-sp.off
	}
	if sp.end <= sp.off || sp.end >= len(sp.src) || sp.src[sp.end] != '(' {
		return
	}
	depth := 0
	for i := sp.end; i < len(sp.src) && sp.src[i] != '\n'; i++ {
		switch sp.src[i] {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				sp.endCol += i + 1 - sp.end
				sp.end = i + 1
				return
			}
		}
	}
}

// lineCol is the 1-based line and byte column of off in src.
func lineCol(src []byte, off int) (int, int) {
	off = min(max(off, 0), len(src))
	line := 1 + bytes.Count(src[:off], []byte("\n"))
	return line, off - bytes.LastIndexByte(src[:off], '\n')
}

// diagnostic is the error msg at pos in cf, at its source, with what vuka
// can tell about it (info and pkg are nil for a syntax error).
func (c *checker) diagnostic(fset *token.FileSet, files []*checkedFile, cf *checkedFile, pos token.Pos, msg string, info *types.Info, pkg *types.Package) diag.Diagnostic {
	path := nodesAt(cf.ast, pos)
	end := token.NoPos
	var call *ast.CallExpr
	first, _, _ := strings.Cut(msg, "\n")
	switch {
	case strings.HasPrefix(first, "not enough arguments in call to ") || strings.HasPrefix(first, "too many arguments in call to "):
		for i := len(path) - 1; i >= 0; i-- {
			if ce, ok := path[i].(*ast.CallExpr); ok {
				call = ce
				break
			}
		}
		if call != nil {
			pos, end = call.Pos(), call.End()
		}
	case strings.HasPrefix(first, "undefined: "):
		if n := len(path); n >= 2 {
			if sel, ok := path[n-2].(*ast.SelectorExpr); ok && path[n-1] == sel.Sel {
				pos, end = sel.Pos(), sel.End()
			}
		}
	}
	sp := c.toSource(fset, cf, pos, end)
	if call != nil {
		sp.withArgs()
	}
	h := hintInput{msg: msg, src: sp.src, from: sp.off, to: sp.end, file: sp.file}
	if g := cf.gen; g != nil {
		h.names = g.Names
		if g.Map != nil {
			h.respell = func(m string) string { return g.Map.Message(sp.line, m) }
		}
	}
	h.markup = sp.src != nil && sp.off >= 0 && inMarkup(sp.file, sp.src, sp.off)
	h.modRoot, h.modPath = c.in.root, c.in.modPath
	if info != nil {
		h.types = &typeFacts{info: info, pkg: pkg, pos: pos, call: call, path: path, files: files}
	}
	hint := goHints(h)
	if from, to := hint.span[0], hint.span[1]; to > from && sp.src != nil {
		if l, c := lineCol(sp.src, from); l == sp.line {
			sp.col, sp.endCol = c, c+to-from
		}
	}
	d := diag.Diagnostic{Code: "E0002", Message: hint.msg, Help: hint.help,
		Span: diag.Span{File: sp.file, Line: sp.line, Col: sp.col, EndCol: sp.endCol, Label: hint.label}}
	if hint.note != "" {
		d.Message += "\n" + hint.note
	}
	return d
}

// nodesAt is the nodes of f enclosing pos, outermost first.
func nodesAt(f *ast.File, pos token.Pos) []ast.Node {
	var path []ast.Node
	if f == nil {
		return nil
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil || pos < n.Pos() || pos > n.End() {
			return false
		}
		path = append(path, n)
		return true
	})
	return path
}

// goScripts matches a .vuka.html file's script and style elements, whose
// text isn't markup.
var notMarkup = regexp.MustCompile(`(?s)<(script|style)\b[^>]*>.*?</(script|style)>`)

// inMarkup reports whether off in the source of file is in a template's
// markup, where the request's ctx is in scope.
func inMarkup(file string, src []byte, off int) bool {
	if !strings.HasSuffix(file, ".vuka.html") {
		return false
	}
	for _, m := range notMarkup.FindAllIndex(src, -1) {
		if off >= m[0] && off < m[1] {
			return false
		}
	}
	return true
}

// failedBuild says why a build failed with err: its report, when it has
// one (then the error is said), else err.
func failedBuild(w io.Writer, rep *buildReport, err error, o reportOptions) error {
	if rep == nil || rep.empty() {
		return err
	}
	writeReport(w, rep, o)
	return exitStatus(1)
}

// writeReport writes the report, as JSON under -diag=json.
func writeReport(w io.Writer, rep *buildReport, o reportOptions) {
	if diagJSON {
		var ds []diag.Diagnostic
		for _, g := range rep.groups {
			ds = append(ds, g.diags...)
		}
		writeDiags(w, append(ds, rep.importers...), false)
		for _, l := range rep.raw {
			io.WriteString(w, strings.TrimSuffix(l, "\n")+"\n")
		}
		return
	}
	rep.write(w, o)
}

// reportText is the report as text.
func reportText(rep *buildReport, o reportOptions) string {
	var b bytes.Buffer
	rep.write(&b, o)
	return b.String()
}

// blockWriter passes the go command's output on but for its packages'
// blocks of errors, which it keeps for report.
type blockWriter struct {
	next    io.Writer
	respell func(string) string
	buf     []byte
	blocks  []goBlock
}

func (b *blockWriter) Write(p []byte) (int, error) {
	b.buf = append(b.buf, p...)
	for {
		i := bytes.IndexByte(b.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := string(b.buf[:i+1])
		b.buf = b.buf[i+1:]
		if err := b.line(line); err != nil {
			return len(p), err
		}
	}
}

func (b *blockWriter) line(l string) error {
	if m := blockHead.FindStringSubmatch(l); m != nil {
		b.blocks = append(b.blocks, goBlock{path: m[1], lines: []string{l}})
		return nil
	}
	if n := len(b.blocks); n > 0 && !b.blocks[n-1].closed && blockLine.MatchString(l) {
		b.blocks[n-1].lines = append(b.blocks[n-1].lines, b.respell(l))
		return nil
	}
	if n := len(b.blocks); n > 0 {
		b.blocks[n-1].closed = true
	}
	_, err := io.WriteString(b.next, l)
	return err
}

func (b *blockWriter) flush() {
	if len(b.buf) > 0 {
		b.line(string(b.buf))
		b.buf = nil
	}
}

// report says what the blocks hold: the packages' errors at their source
// when the go command failed (err), else the blocks as they were.
func (b *blockWriter) report(w io.Writer, err error, in buildInput, o reportOptions) {
	if len(b.blocks) == 0 {
		return
	}
	if err != nil {
		if rep := blocksReport(in, b.blocks, nil); !rep.empty() {
			writeReport(w, rep, o)
			return
		}
	}
	for _, blk := range b.blocks {
		for _, l := range blk.lines {
			io.WriteString(w, l)
		}
	}
}

// goPosition matches a "file.go:line:col" in a message.
var goPosition = regexp.MustCompile(`(\S+\.go):(\d+):(\d+)`)

// mapGoPositions rewrites the positions in generated Go that msg names (the
// editor's builds have no line directives) as the places in the source
// they come from, relative to root, and returns those places.
func mapGoPositions(msg string, gens []load.Generated, root string) (string, []diag.Span) {
	var spans []diag.Span
	out := goPosition.ReplaceAllStringFunc(msg, func(m string) string {
		sm := goPosition.FindStringSubmatch(m)
		line, _ := strconv.Atoi(sm[2])
		col, _ := strconv.Atoi(sm[3])
		for i := range gens {
			g := &gens[i]
			if g.Map == nil || g.Source == "" || g.From == nil || !sameFile(g.Target, sm[1]) {
				continue
			}
			s, _, ok := g.Map.ToSource(offsetAt(g.Src, line, col))
			if !ok {
				break
			}
			sp := diag.Span{File: g.Source}
			sp.Line, sp.Col = lineCol(g.From, s)
			spans = append(spans, sp)
			file := g.Source
			if rel, err := filepath.Rel(root, file); err == nil && !strings.HasPrefix(rel, "..") {
				file = filepath.ToSlash(rel)
			}
			return fmt.Sprintf("%s:%d:%d", file, sp.Line, sp.Col)
		}
		return m
	})
	return out, spans
}
