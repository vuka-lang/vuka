package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/scanner"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vuka-lang/vuka/internal/load"
)

// compileAssets turns a module's declared styles and scripts into what the
// app serves: each component's stylesheets, scoped, in
// .vuka/tailwind/scoped/<scope>.css for Tailwind's entry to import after
// the global ones, and its scripts bundled into a module. It warns on a
// selector matching nothing in its component's markup.

const (
	scopedDir      = ".vuka/tailwind/scoped"
	assetsDir      = ".vuka/assets"
	assetsEnv      = "VUKA_ASSETS"
	assetsGoName   = "vuka_assets_gen.go"
	assetsManifest = "manifest.json"
)

// compiled is what compileAssets made.
type compiled struct {
	css      []string // the stylesheets Tailwind's entry imports, in order
	plain    []byte   // all of them, for an app without Tailwind
	bundles  []*bundle
	manifest []byte
}

// manifestEntry is a component's line of the manifest web reads
// (web.RegisterAssets, or VUKA_ASSETS's manifest.json).
type manifestEntry struct {
	Name    string   `json:"name"`
	Styles  []string `json:"styles,omitempty"`
	Scripts []string `json:"scripts,omitempty"`
	Module  string   `json:"module,omitempty"`
	Methods []string `json:"methods,omitempty"`
	CSS     int      `json:"css,omitempty"`
	// Type is a live component's struct type as reflect spells it, and
	// Pushes the methods its scripts push by name (live.AllowPush).
	Type   string   `json:"type,omitempty"`
	Pushes []string `json:"pushes,omitempty"`
}

type manifestFile struct {
	Scopes map[string]*manifestEntry `json:"scopes,omitempty"`
	Global *manifestEntry            `json:"global,omitempty"`
}

func compileAssets(pa *pageAssets, dev bool, warn io.Writer) (*compiled, error) {
	out := &compiled{}
	if pa.empty() {
		return out, nil
	}
	dir := filepath.Join(pa.root, filepath.FromSlash(scopedDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := writeIfChanged(filepath.Join(pa.root, ".vuka", ".gitignore"), []byte("*\n")); err != nil {
		return nil, err
	}
	mf := manifestFile{Scopes: map[string]*manifestEntry{}}
	written := map[string]bool{}
	var errs []error
	var plain bytes.Buffer

	g := pa.global
	if len(g.styles) > 0 || len(g.scripts) > 0 {
		mf.Global = &manifestEntry{Name: "app", Styles: labels(g.styles), Scripts: labels(g.scripts)}
	}
	var inline strings.Builder
	for _, s := range g.styles {
		if s.path != "" {
			out.css = append(out.css, s.path)
			data, err := os.ReadFile(s.path)
			if err != nil {
				errs = append(errs, err)
			}
			plain.Write(data)
			plain.WriteByte('\n')
			mf.Global.CSS += len(data)
			continue
		}
		fmt.Fprintf(&inline, "/* %s */\n%s\n", s.label, s.inline)
	}
	if inline.Len() > 0 {
		f := filepath.Join(dir, "app.css")
		written[f] = true
		if err := writeIfChanged(f, []byte(inline.String())); err != nil {
			return nil, err
		}
		out.css = append(out.css, f)
		plain.WriteString(inline.String())
		mf.Global.CSS += inline.Len()
	}

	for _, c := range pa.comps {
		e := &manifestEntry{Name: c.goName(), Styles: labels(c.styles), Scripts: labels(c.scripts)}
		mf.Scopes[c.id] = e
		if len(c.styles) == 0 {
			continue
		}
		css, names, err := scopeComponent(c)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		f := filepath.Join(dir, c.id+".css")
		written[f] = true
		if err := writeIfChanged(f, []byte(css)); err != nil {
			return nil, err
		}
		out.css = append(out.css, f)
		plain.WriteString(css)
		e.CSS = len(css)
		if warn != nil {
			checkSelectors(pa, c, names, warn)
		}
	}
	for _, c := range append([]*assetComp{g}, pa.comps...) {
		b, err := bundleScripts(pa.root, c, dev)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if b == nil {
			continue
		}
		out.bundles = append(out.bundles, b)
		e := mf.Global
		if c != g {
			e = mf.Scopes[c.id]
		}
		e.Module, e.Methods, e.Pushes = b.name, b.methods, b.pushes
		if len(b.pushes) > 0 {
			e.Type = goTypeName(c)
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	entries, _ := os.ReadDir(dir)
	for _, en := range entries {
		if f := filepath.Join(dir, en.Name()); !written[f] {
			os.Remove(f)
		}
	}
	out.plain = plain.Bytes()
	var err error
	out.manifest, err = json.Marshal(mf)
	return out, err
}

func labels(srcs []assetSrc) []string {
	var out []string
	for _, s := range srcs {
		out = append(out, s.label)
	}
	return out
}

// scopeComponent is c's stylesheets scoped, in order, and the names their
// selectors require.
func scopeComponent(c *assetComp) (string, []cssName, error) {
	texts := make([]string, len(c.styles))
	frames := map[string]string{}
	for i, s := range c.styles {
		texts[i] = s.inline
		if s.path != "" {
			data, err := os.ReadFile(s.path)
			if err != nil {
				return "", nil, err
			}
			texts[i] = string(data)
		}
		keyframesOf(texts[i], c.id, frames)
	}
	var b strings.Builder
	var names []cssName
	type labelled struct {
		cssName
		label string
	}
	for i, s := range c.styles {
		label, base := s.label, 0
		if s.path == "" {
			label, _, _ = strings.Cut(s.label, ":")
			base = s.line
		}
		css, ns, err := scopeCSS(texts[i], s.dir, label, base, c.id, frames)
		if err != nil {
			return "", nil, err
		}
		fmt.Fprintf(&b, "/* %s: %s */\n%s\n", c.goName(), s.label, css)
		for _, n := range ns {
			n.name = label + "\x00" + n.name
			names = append(names, n)
		}
	}
	return b.String(), names, nil
}

// checkSelectors warns of each class, id or element c's selectors require
// that its markup never writes.
func checkSelectors(pa *pageAssets, c *assetComp, names []cssName, w io.Writer) {
	words, tags := markupNames(c)
	seen := map[string]bool{}
	for _, n := range names {
		label, name, _ := strings.Cut(n.name, "\x00")
		n.name = name
		var have []string
		switch n.kind {
		case 0:
			have = tags
		default:
			have = words
		}
		if slices.Contains(have, n.name) {
			continue
		}
		key := fmt.Sprintf("%s:%d:%s", label, n.line, n)
		if seen[key] {
			continue
		}
		seen[key] = true
		msg := fmt.Sprintf("%s:%d: %s matches no element in %s", label, n.line, n, c.name)
		if s := closest(n.name, have); s != "" {
			msg += "; did you mean " + cssName{kind: n.kind, name: s}.String() + "?"
		}
		fmt.Fprintln(w, msg)
	}
}

var (
	elementTag = regexp.MustCompile(`<([a-z][a-z0-9-]*)[\s/>]`)
	quotedText = regexp.MustCompile("\"(?:[^\"\\\\\\n]|\\\\.)*\"|`[^`]*`|'(?:[^'\\\\\\n]|\\\\.)*'")
)

// markupNames are the words of the strings c's markup and scripts hold
// (the classes and ids it can write) and the elements it writes.
func markupNames(c *assetComp) (words, tags []string) {
	var text strings.Builder
	if c.templ != "" {
		src, _ := os.ReadFile(c.templ)
		text.WriteString(templBody(string(src), c.name))
	} else {
		entries, _ := os.ReadDir(c.dir)
		for _, e := range entries {
			if n := e.Name(); strings.HasSuffix(n, ".vuka") && !strings.HasSuffix(n, "_test.vuka") {
				src, _ := os.ReadFile(filepath.Join(c.dir, n))
				text.WriteString(declText(src, c.name))
			}
		}
	}
	for _, s := range c.scripts {
		src := []byte(s.inline)
		if s.path != "" {
			src, _ = os.ReadFile(s.path)
		}
		for _, q := range quotedText.FindAllString(string(src), -1) {
			text.WriteString(" " + q)
		}
	}
	t := text.String()
	for _, m := range elementTag.FindAllStringSubmatch(t, -1) {
		tags = append(tags, m[1])
	}
	for _, q := range quotedText.FindAllString(t, -1) {
		words = append(words, strings.Fields(q[1:len(q)-1])...)
	}
	slices.Sort(tags)
	slices.Sort(words)
	return slices.Compact(words), slices.Compact(tags)
}

// templBody is the source of the templ component name declares.
func templBody(src, name string) string {
	for _, m := range templDeclRe.FindAllStringSubmatchIndex(src, -1) {
		if src[m[2]:m[3]] != name {
			continue
		}
		end := strings.Index(src[m[0]:], "\n}")
		if end < 0 {
			return src[m[0]:]
		}
		return src[m[0] : m[0]+end]
	}
	return ""
}

// declText is the source of a .vuka file's declarations of name — its
// methods, or the function — and of its package-level constants and
// variables.
func declText(src []byte, name string) string {
	fset := token.NewFileSet()
	tf := fset.AddFile("", -1, len(src))
	var s scanner.Scanner
	s.Init(tf, src, func(token.Position, string) {}, 0)
	var toks []wtok
	for {
		pos, t, lit := s.Scan()
		if t == token.EOF {
			break
		}
		toks = append(toks, wtok{tf.Offset(pos), t, lit})
	}
	var b strings.Builder
	depth, start, keep := 0, -1, false
	for i, t := range toks {
		switch t.tok {
		case token.LPAREN, token.LBRACE, token.LBRACK:
			depth++
		case token.RPAREN, token.RBRACE, token.RBRACK:
			depth--
		case token.FUNC, token.CONST, token.VAR, token.TYPE, token.IMPORT:
			if depth != 0 {
				continue
			}
			if start >= 0 && keep {
				b.WriteString(string(src[start:t.off]))
			}
			start, keep = t.off, t.tok == token.CONST || t.tok == token.VAR
			if t.tok == token.FUNC {
				keep = funcDeclName(toks, i) == name
			}
		}
	}
	if start >= 0 && keep {
		b.WriteString(string(src[start:]))
	}
	return b.String()
}

// funcDeclName is what transpile's declName is: a method's receiver type,
// else the function's name.
func funcDeclName(toks []wtok, i int) string {
	if i+1 >= len(toks) {
		return ""
	}
	switch t := toks[i+1]; t.tok {
	case token.IDENT:
		return t.lit
	case token.LPAREN:
		name, d := "", 0
		for k := i + 2; k < len(toks); k++ {
			switch toks[k].tok {
			case token.LPAREN, token.LBRACK:
				d++
			case token.RBRACK:
				d--
			case token.RPAREN:
				if d == 0 {
					return name
				}
				d--
			case token.IDENT:
				if d == 0 {
					name = toks[k].lit
				}
			}
		}
	}
	return ""
}

// assetsGo is the file embedding the compiled assets in a main package:
// the manifest and each module, registered with web.
func assetsGo(c *compiled) []byte {
	var b strings.Builder
	b.WriteString("// Code generated by vuka: the app's component styles and scripts. DO NOT EDIT.\n\npackage main\n\n")
	b.WriteString("import vukaweb \"" + webPath + "\"\n\nfunc init() {\n\tvukaweb.RegisterAssets([]byte(vukaAssets))\n")
	for i, bn := range c.bundles {
		fmt.Fprintf(&b, "\tvukaweb.RegisterScript(%q, []byte(vukaScript%d))\n", bn.name, i)
	}
	b.WriteString("}\n\nconst vukaAssets = " + strconv.Quote(string(c.manifest)) + "\n")
	for i, bn := range c.bundles {
		fmt.Fprintf(&b, "\nconst vukaScript%d = %s\n", i, strconv.Quote(string(bn.js)))
	}
	return []byte(b.String())
}

// hasWebFunc reports whether web, in its module directory, declares fn.
func hasWebFunc(dir, fn string) bool {
	if dir == "" {
		return false
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, f := range files {
		if src, err := os.ReadFile(f); err == nil && bytes.Contains(src, []byte("func "+fn+"(")) {
			return true
		}
	}
	return false
}

// devAssets keeps a module's compiled assets up to date for vuka run: the
// scoped stylesheets Tailwind's watcher imports, and in .vuka/assets the
// modules and the manifest the app reads (VUKA_ASSETS). It looks at the
// files the assets name every 200ms; a rebuild hands it the module's new
// declarations (set).
type devAssets struct {
	root   string
	stderr io.Writer

	mu      sync.Mutex
	pa      *pageAssets
	files   map[string]time.Time
	css     []string // what the entry imports
	plain   string   // the stylesheet written without Tailwind, if any
	warned  string   // the last warnings, said again only when they change
	entry   func()   // rewrites Tailwind's entry when the stylesheets change
	stop    chan struct{}
	stopped sync.Once
}

func newDevAssets(root string, stderr io.Writer) *devAssets {
	d := &devAssets{root: root, stderr: stderr, stop: make(chan struct{})}
	os.MkdirAll(filepath.Join(root, filepath.FromSlash(assetsDir), "c"), 0o755)
	go d.watch()
	return d
}

// dir is the directory the app reads (VUKA_ASSETS).
func (d *devAssets) dir() string { return filepath.Join(d.root, filepath.FromSlash(assetsDir)) }

// set compiles the assets pa declares.
func (d *devAssets) set(pa *pageAssets) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pa = pa
	d.compile()
}

// stylesheets is what Tailwind's entry imports.
func (d *devAssets) stylesheets() []string {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return slices.Clone(d.css)
}

func (d *devAssets) onEntry(f func()) {
	d.mu.Lock()
	d.entry = f
	d.mu.Unlock()
}

// compile compiles and writes everything; d.mu is held.
func (d *devAssets) compile() {
	files := map[string]time.Time{}
	pa := d.pa
	if pa == nil {
		pa = &pageAssets{root: d.root, global: &assetComp{}}
	}
	for _, c := range append([]*assetComp{pa.global}, pa.comps...) {
		for _, s := range append(slices.Clone(c.styles), c.scripts...) {
			if s.path != "" {
				fi, _ := os.Stat(s.path)
				if fi != nil {
					files[s.path] = fi.ModTime()
				}
			}
		}
	}
	d.files = files
	var warn bytes.Buffer
	out, err := compileAssets(pa, true, &warn)
	if w := warn.String(); w != d.warned {
		d.warned = w
		io.WriteString(d.stderr, w)
	}
	if err != nil {
		fmt.Fprintln(d.stderr, err)
		return
	}
	dir := d.dir()
	keep := map[string]bool{}
	for _, b := range out.bundles {
		keep[b.name] = true
		writeIfChanged(filepath.Join(dir, "c", b.name), b.js)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "c"))
	for _, e := range entries {
		if !keep[e.Name()] {
			os.Remove(filepath.Join(dir, "c", e.Name()))
		}
	}
	writeIfChanged(filepath.Join(dir, assetsManifest), out.manifest)
	if d.plain != "" {
		writeIfChanged(d.plain, out.plain)
	}
	if !slices.Equal(out.css, d.css) {
		d.css = out.css
		if d.entry != nil {
			go d.entry()
		}
	}
}

// watch compiles again when a file the assets name changes.
func (d *devAssets) watch() {
	for {
		select {
		case <-d.stop:
			return
		case <-time.After(200 * time.Millisecond):
		}
		d.mu.Lock()
		for f, mod := range d.files {
			if fi, err := os.Stat(f); err == nil && !fi.ModTime().Equal(mod) || err != nil {
				d.compile()
				break
			}
		}
		d.mu.Unlock()
	}
}

func (d *devAssets) close() {
	if d != nil {
		d.stopped.Do(func() { close(d.stop) })
	}
}

// moduleAssets finds the module's assets and reports what is wrong with
// them; nil when it declares none.
func moduleAssets(root string) (*pageAssets, error) {
	_, modPath, err := load.ModuleRoot(root)
	if err != nil {
		return nil, err
	}
	return findAssets(root, modPath)
}

// goTypeName is c's type as reflect spells it: main.Counter in a main
// package, else its import path and name.
func goTypeName(c *assetComp) string {
	entries, _ := os.ReadDir(c.dir)
	for _, e := range entries {
		if n := e.Name(); strings.HasSuffix(n, ".vuka") || strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, "_test.go") {
			src, _ := os.ReadFile(filepath.Join(c.dir, n))
			if m := pkgClause.FindSubmatch(src); m != nil && string(m[1]) == "main" {
				return "main." + c.name
			}
		}
	}
	return c.importPath + "." + c.name
}
