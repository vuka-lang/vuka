package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// Static files linked with web.Asset. The app's app.Static mounts are found
// in its source as web.Frontend's are: app.Static("/prefix", fsys) with a
// literal prefix, fsys an embed.FS variable of the package, or fs.Sub of
// one with a literal directory (in the call, or assigned to the variable
// the call names). A mount of anything else (os.DirFS, a value built
// elsewhere) is read at run time: its files are neither hashed nor checked.
// vuka build hashes the files of each embedded mount into the names the app
// serves cached for good (web.RegisterStatic), and vuka build and vet check
// each literal web.Asset / web.AssetURL path against them (E0429); vuka vet
// warns on a hard-coded path under a mount in markup (W0404), which vuka fix
// static-paths and the editor's quick fix wrap in web.Asset.

const staticGoName = "vuka_static_gen.go"

// staticMount is an app.Static call.
type staticMount struct {
	prefix string            // the URL prefix, "/static"
	dir    string            // the directory served, when embedded
	files  map[string]string // embedded: path under dir (slashes) → file
	at     string            // the call's file
}

func (m staticMount) embedded() bool { return m.files != nil }

// urlOf is the URL path a file rel of the mount is served at.
func (m staticMount) urlOf(rel string) string {
	return strings.TrimSuffix(m.prefix, "/") + "/" + rel
}

// findStaticMounts are the app.Static calls of the module at root.
func findStaticMounts(root string) []staticMount {
	var out []staticMount
	walkSources(root, func(p, name string) error {
		ext := filepath.Ext(name)
		if ext != ".go" && ext != ".vuka" || strings.HasSuffix(name, "_test"+ext) || name == staticGoName {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil || !bytes.Contains(src, []byte(`"`+webPath+`"`)) || !bytes.Contains(src, []byte(".Static(")) {
			return nil
		}
		for _, c := range staticCalls(src) {
			m := staticMount{prefix: c.prefix, at: p}
			if c.embed != "" {
				if pats := embedPatterns(filepath.Dir(p), c.embed); pats != nil {
					m.dir = filepath.Join(filepath.Dir(p), filepath.FromSlash(c.sub))
					m.files = embeddedFiles(filepath.Dir(p), pats, c.sub)
				}
			}
			out = append(out, m)
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].prefix) > len(out[j].prefix) })
	return out
}

type staticCall struct{ prefix, embed, sub string }

// staticCalls are a source's x.Static("prefix", fsys) calls, with the
// embed.FS variable and directory fsys names when it names one.
func staticCalls(src []byte) []staticCall {
	full, _ := scanFile("", src)
	toks := dropNewlineSemis(full)
	var out []staticCall
	for i := 1; i+2 < len(toks); i++ {
		if toks[i].tok != token.PERIOD || toks[i+1].tok != token.IDENT || toks[i+1].lit != "Static" || toks[i+2].tok != token.LPAREN {
			continue
		}
		args := callArgs(toks, i+2)
		if len(args) != 2 || args[0][1]-args[0][0] != 1 || toks[args[0][0]].tok != token.STRING {
			continue
		}
		prefix, err := strconv.Unquote(toks[args[0][0]].lit)
		if err != nil {
			continue
		}
		c := staticCall{prefix: "/" + strings.Trim(prefix, "/")}
		arg := toks[args[1][0]:args[1][1]]
		if e, s, ok := subCall(arg); ok {
			c.embed, c.sub = e, s
		} else if len(arg) == 1 && arg[0].tok == token.IDENT {
			c.embed = arg[0].lit
			if e, s, ok := assignedSub(toks[:i], arg[0].lit); ok {
				c.embed, c.sub = e, s
			}
		}
		out = append(out, c)
	}
	return out
}

// subCall finds Sub(v, "dir") in toks.
func subCall(toks []wtok) (string, string, bool) {
	for j := 0; j+5 < len(toks); j++ {
		if toks[j].tok == token.IDENT && toks[j].lit == "Sub" && toks[j+1].tok == token.LPAREN && toks[j+2].tok == token.IDENT &&
			toks[j+3].tok == token.COMMA && toks[j+4].tok == token.STRING && toks[j+5].tok == token.RPAREN {
			if s, err := strconv.Unquote(toks[j+4].lit); err == nil {
				return toks[j+2].lit, strings.Trim(path.Clean("/"+s), "/"), true
			}
		}
	}
	return "", "", false
}

// assignedSub finds the last `name[, _] := …Sub(v, "dir")…` before the call.
func assignedSub(toks []wtok, name string) (string, string, bool) {
	for j := len(toks) - 2; j >= 0; j-- {
		if toks[j].tok != token.IDENT || toks[j].lit != name || j > 0 && toks[j-1].tok == token.PERIOD {
			continue
		}
		k := j + 1
		for k < len(toks) && (toks[k].tok == token.COMMA || toks[k].tok == token.IDENT) {
			k++
		}
		if k >= len(toks) || toks[k].tok != token.DEFINE && toks[k].tok != token.ASSIGN {
			continue
		}
		end := k
		for end < len(toks) && toks[end].tok != token.SEMICOLON {
			end++
		}
		return subCall(toks[k:end])
	}
	return "", "", false
}

// embeddedFiles are the files patterns embed in dir that lie under sub,
// keyed by their path under sub; the dotfiles web never serves left out.
func embeddedFiles(dir string, patterns []string, sub string) map[string]string {
	out := map[string]string{}
	add := func(file string, all bool) {
		rel, err := filepath.Rel(dir, file)
		if err != nil {
			return
		}
		rel = filepath.ToSlash(rel)
		if sub != "" {
			var ok bool
			if rel, ok = strings.CutPrefix(rel, sub+"/"); !ok {
				return
			}
		}
		for _, seg := range strings.Split(rel, "/") {
			if strings.HasPrefix(seg, ".") || !all && strings.HasPrefix(seg, "_") {
				return
			}
		}
		out[rel] = file
	}
	for _, p := range patterns {
		all := strings.HasPrefix(p, "all:")
		matches, _ := filepath.Glob(filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(p, "all:"))))
		for _, m := range matches {
			fi, err := os.Stat(m)
			if err != nil {
				continue
			}
			if !fi.IsDir() {
				add(m, true)
				continue
			}
			load.WalkDir(m, func(q string, d fs.DirEntry, err error) error {
				if err == nil && d.Type().IsRegular() {
					add(q, all)
				}
				return nil
			})
		}
	}
	return out
}

// staticURLs are the URL paths of the embedded mounts' files, to their files.
func staticURLs(mounts []staticMount) map[string]string {
	out := map[string]string{}
	for _, m := range mounts {
		for rel, f := range m.files {
			if _, ok := out[m.urlOf(rel)]; !ok {
				out[m.urlOf(rel)] = f
			}
		}
	}
	return out
}

// staticManifest is each embedded file's URL path to its content-hashed
// one. A stylesheet's hash covers the hashed names of the files its url()s
// reach, so a new image gives the stylesheet linking it a new name too.
func staticManifest(mounts []staticMount) (map[string]string, error) {
	files := staticURLs(mounts)
	urls := slices.Sorted(func(yield func(string) bool) {
		for u := range files {
			if !yield(u) {
				return
			}
		}
	})
	out := map[string]string{}
	var sheets []string
	for _, u := range urls {
		if precompressedSibling(u, files) {
			continue
		}
		if path.Ext(u) == ".css" {
			sheets = append(sheets, u)
			continue
		}
		data, err := os.ReadFile(files[u])
		if err != nil {
			return nil, err
		}
		out[u] = hashedName(u, data)
	}
	for _, u := range sheets {
		data, err := os.ReadFile(files[u])
		if err != nil {
			return nil, err
		}
		out[u] = hashedName(u, append(data, rewriteCSSURLs(data, u, out)...))
	}
	return out, nil
}

func precompressedSibling(u string, files map[string]string) bool {
	for _, ext := range []string{".br", ".gz"} {
		if base, ok := strings.CutSuffix(u, ext); ok {
			if _, has := files[base]; has {
				return true
			}
		}
	}
	return false
}

// hashedName is u with the first 8 hex digits of data's SHA-256 before its
// extension: /static/logo.png → /static/logo.3f9a1c2b.png.
func hashedName(u string, data []byte) string {
	sum := sha256.Sum256(data)
	h := hex.EncodeToString(sum[:4])
	dir, base := path.Split(u)
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		return dir + base[:i] + "." + h + base[i:]
	}
	return dir + base + "." + h
}

var cssURLRef = regexp.MustCompile(`url\(\s*(["']?)([^"')\s]+)(["']?)\s*\)`)

// rewriteCSSURLs has css's url()s, absolute or relative to the stylesheet
// at at, name the hashed files of manifest. Only the file's name changes.
func rewriteCSSURLs(css []byte, at string, manifest map[string]string) []byte {
	if len(manifest) == 0 || !bytes.Contains(css, []byte("url(")) {
		return css
	}
	return cssURLRef.ReplaceAllFunc(css, func(m []byte) []byte {
		sub := cssURLRef.FindSubmatch(m)
		ref := string(sub[2])
		if strings.Contains(ref, ":") || strings.HasPrefix(ref, "//") || strings.HasPrefix(ref, "#") {
			return m
		}
		clean, rest := ref, ""
		if i := strings.IndexAny(ref, "?#"); i >= 0 {
			clean, rest = ref[:i], ref[i:]
		}
		full := clean
		if !strings.HasPrefix(clean, "/") {
			full = path.Join(path.Dir(at), clean)
		}
		h, ok := manifest[full]
		if !ok {
			return m
		}
		return []byte("url(" + string(sub[1]) + clean[:strings.LastIndexByte(clean, '/')+1] + path.Base(h) + rest + string(sub[3]) + ")")
	})
}

// staticGo is the file registering manifest in a main package.
func staticGo(manifest map[string]string) []byte {
	var b strings.Builder
	b.WriteString("// Code generated by vuka: the content-hashed names of the app's static files. DO NOT EDIT.\n\npackage main\n\n")
	b.WriteString("import vukaweb \"" + webPath + "\"\n\nfunc init() {\n\tvukaweb.RegisterStatic(map[string]string{\n")
	for _, u := range slices.Sorted(func(yield func(string) bool) {
		for u := range manifest {
			if !yield(u) {
				return
			}
		}
	}) {
		b.WriteString("\t\t" + strconv.Quote(u) + ": " + strconv.Quote(manifest[u]) + ",\n")
	}
	b.WriteString("\t})\n}\n")
	return []byte(b.String())
}

// assetLit is a literal path of a web.Asset or web.AssetURL call.
type assetLit struct {
	start, end int // the literal, quotes included
	path       string
}

var assetCallRe = regexp.MustCompile(`\b([A-Za-z_]\w*)\.(?:AssetURL|Asset)\(\s*(?:[A-Za-z_][\w.]*\s*,\s*)?("(?:[^"\\\n]|\\.)*"|` + "`[^`]*`" + `)\s*\)`)

var webImportRe = regexp.MustCompile(`(?m)^\s*(?:import\s+)?(?:([A-Za-z_]\w*)\s+)?"` + regexp.QuoteMeta(webPath) + `"`)

// webName is the name a source imports web by ("web" when it doesn't).
func webName(src []byte) string {
	if m := webImportRe.FindSubmatch(src); m != nil && len(m[1]) > 0 {
		return string(m[1])
	}
	return "web"
}

// assetRefs are src's web.Asset / web.AssetURL calls with a literal path;
// a call in a Go comment or an HTML one is left out.
func assetLits(file string, src []byte) []assetLit {
	if !bytes.Contains(src, []byte("Asset")) {
		return nil
	}
	view := blankSourceComments(file, src)
	name := webName(src)
	var out []assetLit
	for _, m := range assetCallRe.FindAllSubmatchIndex(view, -1) {
		if string(view[m[2]:m[3]]) != name {
			continue
		}
		p, err := strconv.Unquote(string(view[m[4]:m[5]]))
		if err != nil {
			continue
		}
		out = append(out, assetLit{m[4], m[5], p})
	}
	return out
}

var htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

// blankComments is src with its comments as spaces: Go's in a .go or .vuka
// file, HTML's in a .vuka.html one.
func blankSourceComments(file string, src []byte) []byte {
	out := slices.Clone(src)
	if strings.HasSuffix(file, ".html") {
		for _, m := range htmlComment.FindAllIndex(src, -1) {
			blank(out, m[0], m[1])
		}
		return out
	}
	fset := token.NewFileSet()
	tf := fset.AddFile(file, -1, len(src))
	var s scanner.Scanner
	s.Init(tf, src, func(token.Position, string) {}, scanner.ScanComments)
	for {
		pos, t, lit := s.Scan()
		if t == token.EOF {
			break
		}
		if t == token.COMMENT {
			off := tf.Offset(pos)
			blank(out, off, off+len(lit))
		}
	}
	return out
}

func blank(b []byte, from, to int) {
	for i := max(from, 0); i < to && i < len(b); i++ {
		if b[i] != '\n' {
			b[i] = ' '
		}
	}
}

// mountOf is the mount serving URL path p, the one with the longest prefix.
func mountOf(mounts []staticMount, p string) (staticMount, bool) {
	for _, m := range mounts {
		if strings.HasPrefix(p, strings.TrimSuffix(m.prefix, "/")+"/") {
			return m, true
		}
	}
	return staticMount{}, false
}

// checkAssetRef is E0429 when ref names no file of an embedded mount: one
// under its prefix, or under no mount's prefix but near a file's path.
func checkAssetRef(mounts []staticMount, urls map[string]string, ref assetLit) *diag.Diagnostic {
	p := ref.path
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if _, ok := urls[p]; ok {
		return nil
	}
	m, under := mountOf(mounts, p)
	if under && !m.embedded() {
		return nil
	}
	names := slices.Collect(func(yield func(string) bool) {
		for u := range urls {
			if !yield(u) {
				return
			}
		}
	})
	slices.Sort(names)
	near := closest(p, names)
	if !under && (near == "" || editDistance(p, near) > 2) {
		return nil
	}
	d := diag.New("E0429", "%s isn't a static file", p)
	if near != "" {
		d.WithSuggest(near)
	} else if under {
		d.WithHelp("app.Static(%q, …) serves the files of %s", m.prefix, relPathOr(m.dir))
	}
	return d
}

func relPathOr(dir string) string {
	if wd, err := os.Getwd(); err == nil {
		if r, err := filepath.Rel(wd, dir); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
	}
	return dir
}

// staticAttr is a hard-coded URL path under an embedded mount in a markup
// attribute: the value at start:end, quotes included.
type staticAttr struct {
	start, end int
	attr       string
	value      string
}

var (
	staticTagRe = regexp.MustCompile(`<[A-Za-z][\w.:-]*\b[^<>]*>`)
	markupAttr  = regexp.MustCompile(`\s(src|href|srcset|poster|data)\s*=\s*"([^"{}]*)"`)
	goScript    = regexp.MustCompile(`(?s)^\s*(?:<!--.*?-->\s*)*<script\s+lang="go"\s*>.*?</script>`)
)

// staticAttrs are the attributes of src's markup naming a file under an
// embedded mount by hand.
func staticAttrs(file string, src []byte, mounts []staticMount) []staticAttr {
	if !strings.HasSuffix(file, ".vuka.html") && !strings.HasSuffix(file, ".vuka") {
		return nil
	}
	view := blankSourceComments(file, src)
	if loc := goScript.FindIndex(view); loc != nil {
		blank(view, loc[0], loc[1])
	}
	var out []staticAttr
	for _, t := range staticTagRe.FindAllIndex(view, -1) {
		tag := view[t[0]:t[1]]
		for _, a := range markupAttr.FindAllSubmatchIndex(tag, -1) {
			v := string(tag[a[4]:a[5]])
			if !slices.ContainsFunc(staticURLsOf(string(tag[a[2]:a[3]]), v), func(u string) bool {
				m, ok := mountOf(mounts, u)
				return ok && m.embedded()
			}) {
				continue
			}
			out = append(out, staticAttr{t[0] + a[4] - 1, t[0] + a[5] + 1, string(tag[a[2]:a[3]]), v})
		}
	}
	return out
}

// staticURLsOf are the URL paths of an attribute's value: srcset's
// candidates, else the value.
func staticURLsOf(attr, v string) []string {
	if attr != "srcset" {
		return []string{v}
	}
	var out []string
	for _, c := range strings.Split(v, ",") {
		if f := strings.Fields(c); len(f) > 0 {
			out = append(out, f[0])
		}
	}
	return out
}

// assetExpr is the attribute value v written with web.Asset (web named
// name), braces included: {web.Asset("/static/x.png")}, and for srcset each
// candidate under a mount.
func assetExpr(name, attr, v string, mounts []staticMount) string {
	call := func(u string) string { return name + ".Asset(" + strconv.Quote(u) + ")" }
	if attr != "srcset" {
		return "{" + call(v) + "}"
	}
	var parts []string
	pending := ""
	for i, c := range strings.Split(v, ",") {
		if i > 0 {
			pending += ", "
		}
		f := strings.Fields(c)
		if len(f) == 0 {
			continue
		}
		desc := strings.Join(f[1:], " ")
		if desc != "" {
			desc = " " + desc
		}
		if m, ok := mountOf(mounts, f[0]); ok && m.embedded() {
			if pending != "" {
				parts = append(parts, strconv.Quote(pending))
			}
			parts = append(parts, call(f[0]))
			pending = desc
		} else {
			pending += f[0] + desc
		}
	}
	if pending != "" {
		parts = append(parts, strconv.Quote(pending))
	}
	return "{" + strings.Join(parts, " + ") + "}"
}

// staticDiags are the module's E0429s (a literal web.Asset path that names
// no static file) and, with warnings, W0404s (a hard-coded path in markup).
func staticDiags(root string, warnings bool) []diag.Diagnostic {
	mounts := findStaticMounts(root)
	if !slices.ContainsFunc(mounts, staticMount.embedded) {
		return nil
	}
	urls := staticURLs(mounts)
	var out []diag.Diagnostic
	walkSources(root, func(p, name string) error {
		if !sourceFile(name) {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil || bytes.HasPrefix(src, []byte("// Code generated")) {
			return nil
		}
		out = append(out, fileStaticDiags(p, src, mounts, urls, warnings)...)
		return nil
	})
	return out
}

func sourceFile(name string) bool {
	ext := filepath.Ext(name)
	return (ext == ".go" || ext == ".vuka" || strings.HasSuffix(name, ".vuka.html")) && !strings.HasSuffix(name, "_test"+ext) && name != staticGoName
}

func fileStaticDiags(file string, src []byte, mounts []staticMount, urls map[string]string, warnings bool) []diag.Diagnostic {
	var out []diag.Diagnostic
	span := func(s, e int) diag.Span {
		line, col := staticLineCol(src, s)
		return diag.Span{File: file, Line: line, Col: col, EndCol: col + e - s}
	}
	for _, r := range assetLits(file, src) {
		if d := checkAssetRef(mounts, urls, r); d != nil {
			out = append(out, *d.At(span(r.start, r.end)))
		}
	}
	if warnings {
		name := webName(src)
		for _, a := range staticAttrs(file, src, mounts) {
			d := diag.Warn("W0404", "%s=%q is written by hand: web.Asset links it cached for good", a.attr, a.value).
				WithHelp("write %s=%s", a.attr, assetExpr(name, a.attr, a.value, mounts))
			out = append(out, *d.At(span(a.start, a.end)))
		}
	}
	return out
}

func staticLineCol(src []byte, off int) (int, int) {
	line := 1 + bytes.Count(src[:off], []byte("\n"))
	start := bytes.LastIndexByte(src[:off], '\n') + 1
	return line, 1 + off - start
}

// staticPathEdits are the edits writing src's hard-coded static paths with
// web.Asset, importing web when the file doesn't.
func staticPathEdits(file string, src []byte, mounts []staticMount) []spliceEdit {
	attrs := staticAttrs(file, src, mounts)
	if len(attrs) == 0 {
		return nil
	}
	name := webName(src)
	var eds []spliceEdit
	for _, a := range attrs {
		eds = append(eds, spliceEdit{a.start, a.end, assetExpr(name, a.attr, a.value, mounts)})
	}
	if !webImportRe.Match(src) {
		for _, e := range addImports(transpile.HeaderView(src), []importSpec{{path: webPath}}) {
			eds = append(eds, spliceEdit{e.start, e.end, e.text})
		}
	}
	return eds
}

// fixStaticPaths writes the module's hard-coded static paths in markup with
// web.Asset (W0404).
func fixStaticPaths(m *fixModule) ([]fixChange, error) {
	mounts := findStaticMounts(m.root)
	var changes []fixChange
	walkSources(m.root, func(p, name string) error {
		if !strings.HasSuffix(name, ".vuka.html") && !strings.HasSuffix(name, ".vuka") || strings.HasSuffix(name, "_test.vuka") {
			return nil
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		eds := staticPathEdits(p, src, mounts)
		if len(eds) == 0 {
			return nil
		}
		out := spliceEdits(src, eds)
		n := len(staticAttrs(p, src, mounts))
		changes = append(changes, fixChange{
			what:  fmt.Sprintf("link %d static file(s) with web.Asset in %s", n, filepath.ToSlash(relPath(m.root, p))),
			apply: func() error { return os.WriteFile(p, out, 0o644) },
		})
		return nil
	})
	return changes, nil
}

// cachedMounts is findStaticMounts for the editor, kept a few seconds.
var cachedMounts = struct {
	sync.Mutex
	root   string
	at     time.Time
	mounts []staticMount
}{}

func editorMounts(root string) []staticMount {
	cachedMounts.Lock()
	defer cachedMounts.Unlock()
	if cachedMounts.root != root || time.Since(cachedMounts.at) > 3*time.Second {
		cachedMounts.root, cachedMounts.at, cachedMounts.mounts = root, time.Now(), findStaticMounts(root)
	}
	return cachedMounts.mounts
}
