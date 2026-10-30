package main

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/toolchain/tsprops"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// Islands are client components hydrated in the browser, declared by an
// @ui.Island decorator on a Go struct type (the props): bare, the component is
// islands/[<module>/]<Type>.<ext> under the frontend root (inferIsland);
// @ui.Island("path"[, "strategy"]) names it, and "" keeps the inference;
// options follow (ui.Strategy("load"), ui.Layout(…), the latter the app's).
// The vuka command discovers them like it does web.Style/Script (reading the
// decorator from the source), bundles each component file with esbuild into a
// lazy chunk served under /_vuka/i/, records it in the manifest the app reads
// (web.RegisterIsland / VUKA_ASSETS), and emits islands.d.ts typing the
// component against the Go struct. Vanilla (.ts/.js) islands bundle with the
// in-process esbuild; .vue islands use a Node esbuild build (buildVueIslands);
// .tsx/.jsx/.svelte await a later phase. See docs/design/islands.md.

// islandComp is a discovered island: its type name (the island name, as
// ui.Island registers it), the component file, the framework from the file's
// extension, the default strategy the decorator set, and the props the struct
// declares (for islands.d.ts).
type islandComp struct {
	name        string // the Go type's short name — the island name (data-vk-island)
	src         string // the component file, resolved against the declaring file's dir
	rel         string // src relative to the module root, for messages
	label       string // the decorator's file:line, for messages
	framework   string // vanilla | vue | react | svelte (from the extension)
	strategy    string // the decorator's default strategy, "" when none
	props       []islandProp
	imported    bool   // from a .vuka import; its props are the component's own
	bare        string // the decorator's path when it doesn't start with . (found from the frontend root when not beside the file)
	infer       bool   // a bare @ui.Island (or a "" path): the file is islands/[<module>/]<name>.<ext> under the frontend root
	module      string // the module the declaring file is in (modules/<m>/…), for an inferred path
	namedBy     string // the call naming its component in a .go file (ui.RegisterIsland[Chart]); "" for @ui.Island
	dir         string // a .go file's island: its type's package directory
	problem     string // why an inferred path finds no one file
	problemHelp string
	problemCode diag.Code
	off, end    int // the decorator's span in its file, @ to its last token

	layout            bool   // the decorator gives a ui.Layout
	route             string // the type's route decorator: "Get", "Live", … ("" when none)
	routeLabel        string // its file:line
	routeOff, routeAt int    // its span in the file
	live              bool   // the struct embeds ui.Live

	inputs []string // the files its last esbuild bundle read (vanilla)
}

// routeAdvice is what the vuka command suggests of an island's route: a
// live page (ui.Live embedded) is routed with @web.Live.
func (ic *islandComp) routeAdvice() string {
	if ic.live && ic.route != "" && ic.route != "Live" {
		return fmt.Sprintf("%s embeds ui.Live: route it with @web.Live (a live page backed by its component); @web.%s is for a stateless one", ic.name, ic.route)
	}
	return ""
}

// islandExts are the component extensions a bare @ui.Island looks for, in
// the order messages list them.
var islandExts = []string{".vue", ".tsx", ".jsx", ".svelte", ".ts", ".js"}

// islandModule is the module a source file (slash-form, relative to the
// module root) is in: m for modules/m/…, else "".
func islandModule(rel string) string {
	parts := strings.Split(rel, "/")
	if len(parts) >= 3 && parts[0] == "modules" {
		return parts[1]
	}
	return ""
}

// inferIsland finds a bare @ui.Island's component: islands/[<module>/]<name>
// with one of islandExts under frontendRoot. With none or several, src is the
// path without an extension and problem says what is wrong.
func inferIsland(root, frontendRoot, module, name, naming string) (src, problem, help string, code diag.Code) {
	rel := path.Join("islands", module, name)
	base := filepath.Join(frontendRoot, filepath.FromSlash(rel))
	var found []string
	for _, ext := range islandExts {
		if fileExists(base + ext) {
			found = append(found, base+ext)
		}
	}
	shown := func(p string) string {
		if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
		return filepath.ToSlash(p)
	}
	switch len(found) {
	case 1:
		return found[0], "", "", ""
	case 0:
		exts := make([]string, len(islandExts))
		for i, e := range islandExts {
			exts[i] = e[1:]
		}
		return base, fmt.Sprintf("no component at %s.{%s}", shown(base), strings.Join(exts, ",")), "add it there, or name the file with " + naming + "(\"…\")", "E0310"
	}
	names := make([]string, len(found))
	for i, f := range found {
		names[i] = shown(f)
	}
	both := "both match"
	if len(names) > 2 {
		both = "all match"
	}
	return base, fmt.Sprintf("%s and %s %s", strings.Join(names[:len(names)-1], ", "), names[len(names)-1], both), fmt.Sprintf("name one with %s(%q)", naming, rel+path.Ext(found[0])), "E0311"
}

// locate finds an island's file once the frontend root is known: an
// inferred path's (again: it may have been added or removed since), or a
// bare path's from the frontend root when it isn't beside the file.
func (ic *islandComp) locate(root, frontendRoot string) {
	if frontendRoot == "" {
		frontendRoot = root
	}
	if !ic.infer {
		if ic.bare == "" || fileExists(ic.src) {
			return
		}
		if abs := filepath.Join(frontendRoot, filepath.FromSlash(ic.bare)); fileExists(abs) {
			rel, _ := filepath.Rel(root, abs)
			ic.src, ic.rel = abs, filepath.ToSlash(rel)
		}
		return
	}
	ic.src, ic.problem, ic.problemHelp, ic.problemCode = besideGo(root, ic.goDir(root), ic.name, ic.naming())
	if ic.src == "" {
		ic.src, ic.problem, ic.problemHelp, ic.problemCode = inferIsland(root, frontendRoot, ic.module, ic.name, ic.naming())
	}
	rel, _ := filepath.Rel(root, ic.src)
	ic.rel = filepath.ToSlash(rel)
	ic.framework = islandFramework(ic.src)
}

// goDir is the directory of the Go (or .vuka) file declaring the island.
func (ic *islandComp) goDir(root string) string {
	if ic.dir != "" {
		return ic.dir
	}
	if f, _, ok := strings.Cut(ic.label, ":"); ok && f != "" {
		if filepath.IsAbs(f) {
			return filepath.Dir(f)
		}
		return filepath.Join(root, filepath.Dir(filepath.FromSlash(f)))
	}
	return ""
}

// besideGo finds an inferred island's component next to the file declaring
// it, <snake>.<ext> (trend.vue for Trend): "" when there is none there.
func besideGo(root, dir, name, naming string) (src, problem, help string, code diag.Code) {
	if dir == "" {
		return "", "", "", ""
	}
	base := filepath.Join(dir, snake(name))
	var found []string
	for _, ext := range islandExts {
		if fileExists(base + ext) {
			found = append(found, base+ext)
		}
	}
	switch len(found) {
	case 0:
		return "", "", "", ""
	case 1:
		return found[0], "", "", ""
	}
	rels := make([]string, len(found))
	for i, f := range found {
		r, _ := filepath.Rel(root, f)
		rels[i] = filepath.ToSlash(r)
	}
	return base, strings.Join(rels, " and ") + " both match", fmt.Sprintf("name one with %s(%q)", naming, "./"+filepath.Base(found[0])), "E0311"
}

// naming is how a person names the island's component: @ui.Island in a
// .vuka file, the registration's call in a .go one.
func (ic *islandComp) naming() string {
	if ic.namedBy != "" {
		return ic.namedBy
	}
	return "@ui.Island"
}

// candidates are the files an inferred island's path may name.
func (ic *islandComp) candidates() []string {
	if !ic.infer {
		return nil
	}
	base := strings.TrimSuffix(ic.src, filepath.Ext(ic.src))
	if ic.problem != "" {
		base = ic.src
	}
	out := make([]string, len(islandExts))
	for i, e := range islandExts {
		out[i] = base + e
	}
	return out
}

// islandProp is one exported field of an island's struct: its wire name (json
// tag or field name), its TypeScript type, and whether it is optional (a
// pointer field).
type islandProp struct {
	name     string
	goName   string // the field's name in Go
	ts       string
	optional bool
	value    bool   // a plain value, not a function, state or node: a page's component gets a handle for it
	bind     bool   // bind: the page's component may set it
	deferred string // defer: the group whose loader (Load<Group>) sends it after the first render
	named    string // the Go type's name when ts doesn't know it (a struct of the package)
	typ      []wtok // the Go type
}

// islandFramework is the framework a component file's extension names, as
// ui.frameworkOf does: .vue → vue, .tsx/.jsx → react, .svelte → svelte, else
// vanilla (.ts/.js).
func islandFramework(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".vue":
		return "vue"
	case ".tsx", ".jsx":
		return "react"
	case ".svelte":
		return "svelte"
	default:
		return "vanilla"
	}
}

// scanFile tokenizes a source file, keeping the newline-semicolons the Go
// scanner inserts (struct-field parsing needs them), and returns the tokens
// and a function mapping an offset to its 1-based line.
func scanFile(file string, src []byte) ([]wtok, func(off int) int) {
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
		toks = append(toks, wtok{tf.Offset(pos), t, lit})
	}
	return toks, func(off int) int { return tf.Line(tf.Pos(off)) }
}

// dropNewlineSemis is the tokens without the newline-semicolons, as readFile's
// stream has them: what the call finder and decoratedType expect.
func dropNewlineSemis(toks []wtok) []wtok {
	out := make([]wtok, 0, len(toks))
	for _, t := range toks {
		if t.tok == token.SEMICOLON && t.lit == "\n" {
			continue
		}
		out = append(out, t)
	}
	return out
}

// matchParen is the index of the ) matching the ( at lparen, or -1.
func matchParen(toks []wtok, lparen int) int {
	depth := 0
	for j := lparen; j < len(toks); j++ {
		switch toks[j].tok {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACK, token.RBRACE:
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

// callArgs is the top-level argument token ranges [start, end) of a call whose
// ( is at lparen.
func callArgs(toks []wtok, lparen int) [][2]int {
	var args [][2]int
	depth, start := 0, lparen+1
	for j := lparen; j < len(toks); j++ {
		switch toks[j].tok {
		case token.LPAREN, token.LBRACK, token.LBRACE:
			depth++
		case token.RPAREN, token.RBRACK, token.RBRACE:
			depth--
			if depth == 0 {
				if j > start {
					args = append(args, [2]int{start, j})
				}
				return args
			}
		case token.COMMA:
			if depth == 1 {
				args = append(args, [2]int{start, j})
				start = j + 1
			}
		}
	}
	return args
}

// findFrontendRoot resolves the island build's root by scanning the app's
// source for web.IslandRoot("dir"), else the web.Frontend(fsys, root, ...)
// call, mirroring how the nexus CLI reads nexus.Frontend:
// web.FrontendRoot("dir") when present, else the parent of Frontend's root
// argument (frontend/dist → frontend), else — neither — the module root.
// The result is absolute.
func findFrontendRoot(root string) string {
	rel, found := "", false
	walkSources(root, func(p, name string) error {
		if found {
			return nil
		}
		ext := filepath.Ext(name)
		if ext != ".go" && ext != ".vuka" || strings.HasSuffix(name, "_test"+ext) {
			return nil
		}
		src, err := load.ReadSource(p)
		if err != nil || !strings.Contains(string(src), `"`+webPath+`"`) {
			return nil
		}
		if r, ok := frontendRootIn(src); ok {
			rel, found = r, true
		}
		return nil
	})
	if !found || rel == "" {
		return root
	}
	return filepath.Join(root, filepath.FromSlash(rel))
}

// frontendRootIn reads the frontend folder (slash-form, relative to the module
// root) from a source file's web.Frontend call, and whether the call is there.
func frontendRootIn(src []byte) (string, bool) {
	full, _ := scanFile("", src)
	toks := dropNewlineSemis(full)
	webName := ""
	for n, p := range fileImports(toks) {
		if p == webPath {
			webName = n
		}
	}
	if webName == "" {
		return "", false
	}
	clean := func(s string) string { return strings.Trim(path.Clean("/"+filepath.ToSlash(s)), "/") }
	// web.IslandRoot("dir"): an app without a Frontend names it alone.
	for i := 0; i+5 < len(toks); i++ {
		if toks[i].tok == token.IDENT && toks[i].lit == webName && toks[i+1].tok == token.PERIOD &&
			toks[i+2].tok == token.IDENT && toks[i+2].lit == "IslandRoot" && toks[i+3].tok == token.LPAREN &&
			toks[i+4].tok == token.STRING && toks[i+5].tok == token.RPAREN {
			if v, err := strconv.Unquote(toks[i+4].lit); err == nil {
				return clean(v), true
			}
		}
	}
	for i := 0; i+3 < len(toks); i++ {
		if toks[i].tok != token.IDENT || toks[i].lit != webName || toks[i+1].tok != token.PERIOD ||
			toks[i+2].tok != token.IDENT || toks[i+2].lit != "Frontend" || toks[i+3].tok != token.LPAREN {
			continue
		}
		args := callArgs(toks, i+3)
		// web.FrontendRoot("dir") among the options wins.
		for _, a := range args {
			for j := a[0]; j+5 < a[1]; j++ {
				if toks[j].tok == token.IDENT && toks[j].lit == webName && toks[j+1].tok == token.PERIOD &&
					toks[j+2].tok == token.IDENT && toks[j+2].lit == "FrontendRoot" && toks[j+3].tok == token.LPAREN &&
					toks[j+4].tok == token.STRING && toks[j+5].tok == token.RPAREN {
					if v, err := strconv.Unquote(toks[j+4].lit); err == nil {
						return clean(v), true
					}
				}
			}
		}
		// Else the parent of Frontend's root argument (the second one).
		if len(args) >= 2 && args[1][1]-args[1][0] == 1 && toks[args[1][0]].tok == token.STRING {
			if v, err := strconv.Unquote(toks[args[1][0]].lit); err == nil {
				if dir := clean(path.Dir(clean(v))); dir != "" {
					return dir, true
				}
			}
		}
		return "", true // a Frontend call with a root at the module root
	}
	return "", false
}

// readIslands reads the @ui.Island decorators of a source file into pa. A
// decorator whose path isn't a string literal, or that isn't on a type, is an
// error; a missing file or a non-vanilla framework is left for compileAssets
// (dev warn / build error).
func (pa *pageAssets) readIslands(file string, src []byte) []error {
	full, line := scanFile(file, src)
	filt := dropNewlineSemis(full)
	uiName, webName := "", ""
	for name, p := range fileImports(filt) {
		switch p {
		case load.UIPath:
			uiName = name
		case webPath:
			webName = name
		}
	}
	if uiName == "" {
		return nil
	}
	rel, _ := filepath.Rel(pa.root, file)
	rel = filepath.ToSlash(rel)
	dir := filepath.Dir(file)
	var errs []error
	pa.readIslandImports(file, rel, line, filt)
	type route struct {
		verb     string
		off, end int
	}
	routes := map[string]route{}
	for i := 0; webName != "" && i+3 < len(filt); i++ {
		if filt[i].tok != token.ILLEGAL || filt[i].lit != "@" || filt[i+1].lit != webName || filt[i+2].tok != token.PERIOD {
			continue
		}
		switch verb := filt[i+3].lit; verb {
		case "Get", "Post", "Put", "Patch", "Delete", "Handle", "Live":
			end := i + 4
			if end < len(filt) && filt[end].tok == token.LPAREN {
				if rp := matchParen(filt, end); rp >= 0 {
					end = rp + 1
				}
			}
			if name := decoratedType(filt, i); name != "" {
				last := filt[end-1]
				routes[name] = route{verb, filt[i].off, last.off + len(tokenText(last))}
			}
		}
	}
	for i := 0; i+2 < len(filt); i++ {
		if filt[i].tok != token.IDENT || filt[i].lit != uiName || filt[i+1].tok != token.PERIOD ||
			filt[i+2].tok != token.IDENT || filt[i+2].lit != "Island" {
			continue
		}
		if filt[i].off == 0 || src[filt[i].off-1] != '@' {
			continue // a plain ui.Island(...) call, not a decorator
		}
		where := fmt.Sprintf("%s:%d", rel, line(filt[i].off))

		after := i + 3
		var args [][2]int
		if i+3 < len(filt) && filt[i+3].tok == token.LPAREN {
			rp := matchParen(filt, i+3)
			if rp < 0 {
				continue
			}
			args, after = callArgs(filt, i+3), rp+1
		}

		str := func(a [2]int) (string, bool) {
			if a[1]-a[0] != 1 || filt[a[0]].tok != token.STRING {
				return "", false
			}
			v, err := strconv.Unquote(filt[a[0]].lit)
			return v, err == nil
		}
		// A string literal path, then a string literal strategy, then
		// options: ui.Strategy("…"), read here; ui.Layout(…) and any
		// other call, evaluated by the app.
		cp, strategy, layout, strs := "", "", false, 0
		bad := ""
		for _, a := range args {
			if v, ok := str(a); ok {
				switch strs {
				case 0:
					cp = v
				case 1:
					strategy = v
				}
				strs++
				continue
			}
			opt := ""
			if a[1]-a[0] >= 4 && filt[a[0]].lit == uiName && filt[a[0]+1].tok == token.PERIOD && filt[a[0]+3].tok == token.LPAREN {
				opt = filt[a[0]+2].lit
			}
			switch {
			case opt == "Strategy":
				sa := callArgs(filt, a[0]+3)
				v, ok := "", len(sa) == 1
				if ok {
					v, ok = str(sa[0])
				}
				if !ok {
					bad = fmt.Sprintf("%s: %s.Strategy takes a string literal (the vuka command reads it from the source)", where, uiName)
				}
				strategy = v
			case opt == "Layout":
				layout = true
			case filt[a[1]-1].tok == token.RPAREN:
			case strs == 0:
				bad = fmt.Sprintf("%s: @%s.Island takes a string literal path (the vuka command reads it from the source)", where, uiName)
			default:
				bad = fmt.Sprintf("%s: @%s.Island's options are calls: %s.Layout(…), %s.Strategy(\"…\")", where, uiName, uiName, uiName)
			}
		}
		if bad != "" {
			errs = append(errs, diag.New("E0320", "%s", strings.TrimPrefix(bad, where+": ")).At(labelSpan(where)))
			continue
		}
		name := decoratedType(filt, after)
		if name == "" {
			errs = append(errs, diag.New("E0320", "@%s.Island isn't on a type", uiName).At(labelSpan(where)))
			continue
		}
		last := filt[after-1]
		ic := &islandComp{
			off:      filt[i].off - 1,
			end:      last.off + len(tokenText(last)),
			name:     name,
			label:    where,
			strategy: strategy,
			props:    structFields(full, name),
			layout:   layout,
			live:     embeds(full, name, uiName, "Live"),
		}
		if r, ok := routes[name]; ok {
			ic.route, ic.routeOff, ic.routeAt = r.verb, r.off, r.end
			ic.routeLabel = fmt.Sprintf("%s:%d", rel, line(r.off))
		}
		ic.src = cp
		ic.locateArg(pa.root, dir, rel)
		pa.islands = append(pa.islands, ic)
	}
	return errs
}

// embeds reports whether the struct type name of a file (its tokens with
// newlines) embeds pkg.field.
func embeds(full []wtok, name, pkg, field string) bool {
	for i := 0; i+3 < len(full); i++ {
		if full[i].tok != token.TYPE || full[i+1].lit != name || full[i+2].tok != token.STRUCT || full[i+3].tok != token.LBRACE {
			continue
		}
		depth := 0
		for j := i + 3; j < len(full); j++ {
			switch full[j].tok {
			case token.LBRACE, token.LPAREN, token.LBRACK:
				depth++
			case token.RBRACE, token.RPAREN, token.RBRACK:
				if depth--; depth == 0 {
					return false
				}
			case token.IDENT:
				prev := full[j-1].tok
				if depth == 1 && (prev == token.LBRACE || prev == token.SEMICOLON) && full[j].lit == pkg && j+2 < len(full) &&
					full[j+1].tok == token.PERIOD && full[j+2].lit == field && (j+3 >= len(full) || full[j+3].tok == token.SEMICOLON || full[j+3].tok == token.RBRACE || full[j+3].tok == token.STRING) {
					return true
				}
			}
		}
	}
	return false
}

// tokenText is a token's source text.
func tokenText(t wtok) string {
	if t.lit != "" {
		return t.lit
	}
	return t.tok.String()
}

// readIslandImports reads a .vuka file's imports of component files
// (import Counter "./islands/Counter.vue"): islands whose props the compiler
// reads from the component (an @ui.Island struct of the name wins).
func (pa *pageAssets) readIslandImports(file, rel string, line func(int) int, toks []wtok) {
	if !strings.HasSuffix(file, ".vuka") && !strings.HasSuffix(file, ".vuka.html") {
		return
	}
	dir := filepath.Dir(file)
	page, tag, full, live := islandPageOf(file)
	for i := 0; i+1 < len(toks); i++ {
		switch toks[i].tok {
		case token.FUNC, token.TYPE, token.VAR, token.CONST:
			return
		}
		if toks[i].tok != token.STRING || i == 0 || toks[i-1].tok != token.IDENT {
			continue
		}
		p, err := strconv.Unquote(toks[i].lit)
		if err != nil || !tsprops.IsComponent(p) {
			continue
		}
		abs, src, readErr := tsprops.ReadIslandComponent(pa.root, dir, p, load.ReadSource)
		relSrc, _ := filepath.Rel(pa.root, abs)
		ic := &islandComp{
			name:      toks[i-1].lit,
			src:       abs,
			rel:       filepath.ToSlash(relSrc),
			label:     fmt.Sprintf("%s:%d", rel, line(toks[i].off)),
			framework: islandFramework(p),
			imported:  true,
		}
		if readErr != nil {
			ic.problem, ic.problemCode = readErr.Error(), "E0303"
		}
		if readErr == nil {
			if c, err := tsprops.Read(filepath.ToSlash(abs), src, func(q string) ([]byte, error) { return load.ReadSource(filepath.FromSlash(q)) }); err == nil {
				for _, pr := range c.Props {
					ic.props = append(ic.props, islandProp{name: pr.Name, ts: pr.Type.String(), optional: pr.Optional})
				}
			}
		}
		if page != "" && tag == ic.name {
			// An island page: its struct is the island, as an @ui.Island
			// struct is, and its props the struct's.
			ic.name, ic.imported, ic.live, ic.props = page, false, live, structFields(full, page)
		}
		pa.islands = append(pa.islands, ic)
	}
}

// islandPageOf reads a .vuka.html file as an island page's: the struct it
// renders, the tag its markup is, alone, the file's tokens, and whether
// the page is live (its struct embeds ui.Live; a stateless one declares
// no hook); "" when it isn't one.
func islandPageOf(file string) (page, tag string, full []wtok, live bool) {
	if !strings.HasSuffix(file, ".vuka.html") {
		return "", "", nil, false
	}
	src, err := load.ReadSource(file)
	if err != nil {
		return "", "", nil, false
	}
	src = transpile.HeaderView(src)
	sh, _ := transpile.ParseShape(src)
	if sh == nil || sh.Live && !sh.EmbedsLive {
		return "", "", nil, false
	}
	lines := strings.SplitAfter(string(src), "\n")
	if sh.Markup < 0 || sh.Markup >= len(lines) {
		return "", "", nil, false
	}
	m := islandPageTag.FindStringSubmatch(strings.Join(lines[sh.Markup:], " "))
	if m == nil {
		return "", "", nil, false
	}
	full, _ = scanFile(file, []byte(strings.Join(lines[:sh.Markup], "")))
	return sh.Type, m[1], full, sh.EmbedsLive
}

// islandPageTag is an island page's markup: one self-closing tag.
var islandPageTag = regexp.MustCompile(`^\s*<([A-Z]\w*)\b[^<>]*/>\s*$`)

// dedupeIslands keeps one island per name: an @ui.Island struct over an
// import of the same name, the first of several imports.
func dedupeIslands(islands []*islandComp) []*islandComp {
	byName := map[string]*islandComp{}
	var out []*islandComp
	for _, ic := range islands {
		prev, ok := byName[ic.name]
		switch {
		case !ok:
			byName[ic.name] = ic
			out = append(out, ic)
		case prev.imported && !ic.imported:
			*prev = *ic
		}
	}
	return out
}

// structFields parses the exported fields of the struct type `name` declares,
// as islandProps. The slot field (Children ui.Node) and embedded fields are
// left out. It reads the tokens with newline-semicolons, which delimit fields.
func structFields(full []wtok, name string) []islandProp {
	for i := 0; i+2 < len(full); i++ {
		if full[i].tok != token.TYPE || full[i+1].tok != token.IDENT || full[i+1].lit != name {
			continue
		}
		if full[i+2].tok != token.STRUCT || i+3 >= len(full) || full[i+3].tok != token.LBRACE {
			continue
		}
		props := parseStructBody(full, i+3)
		schema := liveSchemaFields(full, name)
		for j := range props {
			if f, ok := schema[props[j].goName]; ok {
				props[j].bind = props[j].bind || f.bind
				if f.group != "" && props[j].deferred == "" {
					props[j].deferred = strings.ToUpper(f.group[:1]) + f.group[1:]
				}
			}
		}
		return props
	}
	return nil
}

// fieldAttrs is what a live page says of a field: bound, deferred to a
// group.
type fieldAttrs struct {
	bind  bool
	group string
}

// readFieldAttrs reads ui.Bind and ui.Defer("group") among a field's
// attributes (their tokens, each an @ attribute or an l.Field argument).
func readFieldAttrs(attrs [][]wtok) fieldAttrs {
	var f fieldAttrs
	for _, a := range attrs {
		for i, w := range a {
			if w.tok != token.IDENT || i+1 < len(a) && a[i+1].tok == token.PERIOD {
				continue
			}
			switch {
			case w.lit == "Bind":
				f.bind = true
			case w.lit == "Defer" && i+2 < len(a) && a[i+1].tok == token.LPAREN && a[i+2].tok == token.STRING:
				f.group, _ = strconv.Unquote(a[i+2].lit)
			}
			break
		}
	}
	return f
}

// liveSchemaFields is what the LiveSchema method of the type name, in a
// file's tokens, says of its fields, by their Go names:
// l.Field(&p.Q, ui.Bind{}) and l.Field(&p.Rows, ui.Defer("lists")).
func liveSchemaFields(full []wtok, name string) map[string]fieldAttrs {
	out := map[string]fieldAttrs{}
	for i := 0; i+2 < len(full); i++ {
		if full[i].tok != token.FUNC || full[i+1].tok != token.LPAREN || full[i+2].tok != token.IDENT {
			continue
		}
		recv, j := full[i+2].lit, i+3
		if j < len(full) && full[j].tok == token.MUL {
			j++
		}
		if j+3 >= len(full) || full[j].lit != name || full[j+1].tok != token.RPAREN || full[j+2].lit != "LiveSchema" || full[j+3].tok != token.LPAREN {
			continue
		}
		end := matchParen(full, j+3)
		for end >= 0 && end < len(full) && full[end].tok != token.LBRACE {
			end++
		}
		if end < 0 || end >= len(full) {
			return out
		}
		depth := 0
		for k := end; k < len(full); k++ {
			switch full[k].tok {
			case token.LBRACE:
				depth++
			case token.RBRACE:
				if depth--; depth == 0 {
					return out
				}
			case token.IDENT:
				if full[k].lit != "Field" || full[k-1].tok != token.PERIOD || k+5 >= len(full) || full[k+1].tok != token.LPAREN ||
					full[k+2].tok != token.AND || full[k+3].lit != recv || full[k+4].tok != token.PERIOD || full[k+5].tok != token.IDENT {
					continue
				}
				var args [][]wtok
				for _, a := range callArgs(full, k+1)[1:] {
					args = append(args, full[a[0]:a[1]])
				}
				f := readFieldAttrs(args)
				prev := out[full[k+5].lit]
				out[full[k+5].lit] = fieldAttrs{bind: prev.bind || f.bind, group: cmp.Or(prev.group, f.group)}
			}
		}
	}
	return out
}

// cutAttrs splits a struct field's tokens from the attributes a .vuka file
// writes after it (Q string @ui.Bind @ui.Defer("g")): the field's, and each
// attribute's, its @ dropped.
func cutAttrs(fl []wtok) ([]wtok, [][]wtok) {
	cut, depth := -1, 0
	var attrs [][]wtok
	for i, w := range fl {
		switch w.tok {
		case token.LBRACE, token.LPAREN, token.LBRACK:
			depth++
		case token.RBRACE, token.RPAREN, token.RBRACK:
			depth--
		case token.ILLEGAL:
			if w.lit == "@" && depth == 0 {
				if cut < 0 {
					cut = i
				}
				attrs = append(attrs, nil)
				continue
			}
		}
		if cut >= 0 {
			attrs[len(attrs)-1] = append(attrs[len(attrs)-1], w)
		}
	}
	if cut < 0 {
		return fl, nil
	}
	return fl[:cut], attrs
}

// parseStructBody parses the fields of the struct whose { is at open.
func parseStructBody(full []wtok, open int) []islandProp {
	var props []islandProp
	depth, start := 0, -1
	field := func(toks []wtok) {
		if len(toks) > 0 {
			props = append(props, fieldProps(toks)...)
		}
	}
	for i := open; i < len(full); i++ {
		switch full[i].tok {
		case token.LBRACE, token.LPAREN, token.LBRACK:
			depth++
			if depth == 1 && i == open {
				start = i + 1
			}
		case token.RBRACE, token.RPAREN, token.RBRACK:
			depth--
			if depth == 0 {
				if start >= 0 && i > start {
					field(full[start:i])
				}
				return props
			}
		case token.SEMICOLON:
			if depth == 1 {
				if start >= 0 && i > start {
					field(full[start:i])
				}
				start = i + 1
			}
		}
	}
	return props
}

// fieldProps turns one struct field's tokens (without its terminating
// semicolon) into props: one per exported name, sharing the field's type. An
// embedded field, or the slot (Children), yields none.
func fieldProps(toks []wtok) []islandProp {
	toks, attrs := cutAttrs(toks)
	fa := readFieldAttrs(attrs)
	tag := ""
	if n := len(toks); n > 0 && toks[n-1].tok == token.STRING {
		if v, err := strconv.Unquote(toks[n-1].lit); err == nil {
			tag = v
		}
		toks = toks[:n-1]
	}
	names, typ := splitNamesType(toks)
	if len(names) == 0 {
		return nil // embedded
	}
	optional, ts := tsType(typ)
	jsonName, skip := "", false
	if len(names) == 1 {
		jsonName, skip = jsonTagName(tag)
	}
	if skip || secretTag(tag) {
		return nil // a password or one-time code is never a prop (ui.Props)
	}
	b, bound := reflect.StructTag(tag).Lookup("bind")
	bound = bound && b != "-" || fa.bind
	group, deferred := reflect.StructTag(tag).Lookup("defer")
	deferred = deferred && group != "-"
	if fa.group != "" {
		group, deferred = fa.group, true
	}
	var out []islandProp
	for _, nm := range names {
		if !token.IsExported(nm) || nm == "Children" {
			continue
		}
		wire := nm
		if jsonName != "" {
			wire = jsonName
		}
		p := islandProp{name: wire, goName: nm, ts: ts, optional: optional, value: plainType(typ), bind: bound, named: localTypeName(typ), typ: typ}
		if deferred {
			p.deferred = group
			if p.deferred == "" {
				p.deferred = nm
			}
			p.deferred = strings.ToUpper(p.deferred[:1]) + p.deferred[1:]
		}
		out = append(out, p)
	}
	return out
}

// splitNamesType splits a field's tokens into its name list and its type. An
// embedded field (no names) returns a nil list and the whole thing as the type.
func splitNamesType(body []wtok) (names []string, typ []wtok) {
	if len(body) == 0 || body[0].tok != token.IDENT {
		return nil, body
	}
	i := 0
	var cand []string
	for i < len(body) && body[i].tok == token.IDENT {
		cand = append(cand, body[i].lit)
		if i+1 < len(body) && body[i+1].tok == token.COMMA {
			i += 2
			continue
		}
		i++
		break
	}
	if i < len(body) && isTypeStart(body[i].tok) {
		return cand, body[i:]
	}
	return nil, body
}

func isTypeStart(t token.Token) bool {
	switch t {
	case token.IDENT, token.MUL, token.LBRACK, token.MAP, token.CHAN,
		token.FUNC, token.INTERFACE, token.STRUCT, token.ARROW, token.LPAREN:
		return true
	}
	return false
}

// tsType maps a Go type's tokens to a TypeScript type and reports whether the
// field is optional (a pointer).
func tsType(typ []wtok) (optional bool, ts string) {
	if len(typ) == 0 {
		return false, "unknown"
	}
	switch typ[0].tok {
	case token.MUL:
		_, inner := tsType(typ[1:])
		return true, inner
	case token.LBRACK:
		if len(typ) > 1 && typ[1].tok == token.RBRACK {
			_, inner := tsType(typ[2:])
			return false, inner + "[]"
		}
		return false, "unknown"
	case token.MAP:
		depth, keyStart := 0, -1
		for i := 1; i < len(typ); i++ {
			switch typ[i].tok {
			case token.LBRACK:
				depth++
				if depth == 1 {
					keyStart = i + 1
				}
			case token.RBRACK:
				if depth == 1 {
					_, k := tsType(typ[keyStart:i])
					_, v := tsType(typ[i+1:])
					return false, "Record<" + k + ", " + v + ">"
				}
				depth--
			}
		}
		return false, "unknown"
	case token.IDENT:
		if len(typ) == 1 {
			return false, basicTS(typ[0].lit)
		}
	}
	return false, "unknown"
}

func basicTS(name string) string {
	switch name {
	case "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16",
		"uint32", "uint64", "uintptr", "float32", "float64", "byte", "rune", "complex64", "complex128":
		return "number"
	case "string":
		return "string"
	case "bool":
		return "boolean"
	}
	return "unknown"
}

// plainType reports whether a field of type typ is a plain value: not a
// function, a ui state, commands or a node.
func plainType(typ []wtok) bool {
	if len(typ) == 0 || typ[0].tok == token.FUNC {
		return false
	}
	if len(typ) >= 3 && typ[0].tok == token.IDENT && typ[0].lit == "ui" && typ[1].tok == token.PERIOD {
		switch typ[2].lit {
		case "State", "Derived", "Commands", "JS", "JSChain", "Node":
			return false
		}
	}
	return true
}

// localTypeName is the name of the type typ (under pointers and slices) when it
// is a plain identifier: a type of the package.
func localTypeName(typ []wtok) string {
	for len(typ) > 0 && (typ[0].tok == token.MUL || typ[0].tok == token.LBRACK && len(typ) > 1 && typ[1].tok == token.RBRACK) {
		if typ[0].tok == token.MUL {
			typ = typ[1:]
		} else {
			typ = typ[2:]
		}
	}
	if len(typ) == 1 && typ[0].tok == token.IDENT && basicTS(typ[0].lit) == "unknown" {
		return typ[0].lit
	}
	return ""
}

// jsonTagName is the wire name a field's struct tag sets, and whether the field
// is skipped (json:"-").
func jsonTagName(tag string) (name string, skip bool) {
	j := reflect.StructTag(tag).Get("json")
	if j == "" {
		return "", false
	}
	n, _, _ := strings.Cut(j, ",")
	if n == "-" {
		return "", true
	}
	return n, false
}

// islandBundle is an island's bundled component chunk.
type islandBundle struct {
	name string // its file name under /_vuka/i/: <lowered-name>.<hash>.js
	js   []byte
}

// bundleIsland bundles an island's component file with esbuild into one ES
// module, minified for a build, with an inline source map in development.
// vuka:island is islandModuleJS; packages resolve from root's node_modules.
func bundleIsland(root string, ic *islandComp, dev bool) (*islandBundle, error) {
	opts := api.BuildOptions{
		EntryPoints:   []string{ic.src},
		Bundle:        true,
		Format:        api.FormatESModule,
		Platform:      api.PlatformBrowser,
		Target:        api.ES2020,
		Write:         false,
		AbsWorkingDir: root,
		LogLevel:      api.LogLevelSilent,
		Charset:       api.CharsetUTF8,
		NodePaths:     []string{filepath.Join(root, "node_modules")},
		Plugins:       []api.Plugin{islandModulePlugin(), recordInputs(&ic.inputs)},
	}
	if dev {
		opts.Sourcemap = api.SourceMapInline
	} else {
		opts.MinifyWhitespace, opts.MinifyIdentifiers, opts.MinifySyntax = true, true, true
	}
	res := api.Build(opts)
	if len(res.Errors) > 0 {
		return nil, islandBuildError(root, ic, res.Errors)
	}
	if len(res.OutputFiles) == 0 {
		return nil, fmt.Errorf("vuka: esbuild wrote nothing for island %s", ic.name)
	}
	js := res.OutputFiles[0].Contents
	sum := sha256.Sum256(js)
	return &islandBundle{name: strings.ToLower(ic.name) + "." + hex.EncodeToString(sum[:4]) + ".js", js: js}, nil
}

// islandBuildError reports esbuild's errors against the island's files.
func islandBuildError(root string, ic *islandComp, msgs []api.Message) error {
	var lines []string
	for _, m := range msgs {
		text := m.Text
		for _, n := range m.Notes {
			if n.Text != "" && !strings.Contains(n.Text, "mark the path") {
				text += " (" + n.Text + ")"
			}
		}
		text = strings.TrimPrefix(text, "[plugin vuka-island] ")
		loc := m.Location
		if loc == nil {
			lines = append(lines, fmt.Sprintf("island %s: %s", ic.name, text))
			continue
		}
		file := loc.File
		if filepath.IsAbs(file) {
			if r, err := filepath.Rel(root, file); err == nil {
				file = r
			}
		}
		lines = append(lines, fmt.Sprintf("%s:%d:%d: %s", filepath.ToSlash(file), loc.Line, loc.Column+1, text))
	}
	return fmt.Errorf("island %s: %s", ic.name, strings.Join(lines, "\n"))
}

var tsIdent = regexp.MustCompile(`^[A-Za-z_$][\w$]*$`)

// islandsDTS is the TypeScript declaration of every island's props, generated
// from the Go structs (the source of truth): a <Name>Props interface each, and
// a combined IslandProps map.
func islandsDTS(islands []*islandComp) []byte {
	if len(islands) == 0 {
		return nil
	}
	sorted := slices.Clone(islands)
	slices.SortFunc(sorted, func(a, b *islandComp) int { return strings.Compare(a.name, b.name) })
	var b strings.Builder
	b.WriteString("// Code generated by vuka from the islands' props. DO NOT EDIT.\n")
	b.WriteString("// An @ui.Island type is the source of truth for its props; an imported\n")
	b.WriteString("// component's are its own, as the compiler read them.\n\n")
	for _, ic := range sorted {
		fmt.Fprintf(&b, "export interface %sProps {\n", ic.name)
		for _, p := range ic.props {
			opt := ""
			if p.optional {
				opt = "?"
			}
			fmt.Fprintf(&b, "\t%s%s: %s;\n", tsKey(p.name), opt, p.ts)
		}
		b.WriteString("}\n\n")
	}
	b.WriteString("export interface IslandProps {\n")
	for _, ic := range sorted {
		fmt.Fprintf(&b, "\t%s: %sProps;\n", tsKey(ic.name), ic.name)
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

// tsKey is a property key, quoted when it isn't a bare identifier.
func tsKey(name string) string {
	if tsIdent.MatchString(name) {
		return name
	}
	return strconv.Quote(name)
}

// diagnostic is what is wrong with an island's file, nil when nothing.
func (ic *islandComp) diagnostic() *diag.Diagnostic {
	if ic.problem != "" {
		return diag.New(ic.problemCode, "island %s: %s", ic.name, ic.problem).At(labelSpan(ic.label)).WithHelp("%s", ic.problemHelp)
	}
	if fi, err := os.Stat(ic.src); err != nil || fi.IsDir() {
		return diag.New("E0310", "island %s: component %s not found", ic.name, ic.rel).At(labelSpan(ic.label)).
			WithHelp("create the file, or correct the path %s names", ic.naming())
	}
	return nil
}
