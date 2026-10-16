package main

import (
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
	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/internal/tsprops"
)

// Islands are client components hydrated in the browser, declared by an
// @ui.Island("path"[, "strategy"]) decorator on a Go struct type (the props).
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
	name      string // the Go type's short name — the island name (data-vk-island)
	src       string // the component file, resolved against the declaring file's dir
	rel       string // src relative to the module root, for messages
	label     string // the decorator's file:line, for messages
	framework string // vanilla | vue | react | svelte (from the extension)
	strategy  string // the decorator's default strategy, "" when none
	props     []islandProp
	imported  bool   // from a .vuka import; its props are the component's own
	bare      string // the decorator's path when it doesn't start with . (found from the frontend root when not beside the file)
}

// islandProp is one exported field of an island's struct: its wire name (json
// tag or field name), its TypeScript type, and whether it is optional (a
// pointer field).
type islandProp struct {
	name     string
	ts       string
	optional bool
	value    bool   // a plain value, not a function, state or node: a page's component gets a handle for it
	bind     bool   // bind: the page's component may set it
	named    string // the Go type's name when ts doesn't know it (a struct of the package)
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
		src, err := os.ReadFile(p)
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
	uiName := ""
	for name, p := range fileImports(filt) {
		if p == load.UIPath {
			uiName = name
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
	for i := 0; i+3 < len(filt); i++ {
		if filt[i].tok != token.IDENT || filt[i].lit != uiName || filt[i+1].tok != token.PERIOD ||
			filt[i+2].tok != token.IDENT || filt[i+2].lit != "Island" || filt[i+3].tok != token.LPAREN {
			continue
		}
		if filt[i].off == 0 || src[filt[i].off-1] != '@' {
			continue // a plain ui.Island(...) call, not a decorator
		}
		where := fmt.Sprintf("%s:%d", rel, line(filt[i].off))

		rp := matchParen(filt, i+3)
		if rp < 0 {
			continue
		}
		args := callArgs(filt, i+3)

		str := func(a [2]int) (string, bool) {
			if a[1]-a[0] != 1 || filt[a[0]].tok != token.STRING {
				return "", false
			}
			v, err := strconv.Unquote(filt[a[0]].lit)
			return v, err == nil
		}
		if len(args) == 0 {
			errs = append(errs, fmt.Errorf("%s: @%s.Island needs the component path as a string literal", where, uiName))
			continue
		}
		cp, ok := str(args[0])
		if !ok {
			errs = append(errs, fmt.Errorf("%s: @%s.Island takes a string literal path (the vuka command reads it from the source)", where, uiName))
			continue
		}
		strategy := ""
		if len(args) >= 2 {
			if v, ok := str(args[1]); ok {
				strategy = v
			}
		}
		name := decoratedType(filt, rp+1)
		if name == "" {
			errs = append(errs, fmt.Errorf("%s: @%s.Island isn't on a type", where, uiName))
			continue
		}
		abs := filepath.Join(dir, filepath.FromSlash(cp))
		relSrc, _ := filepath.Rel(pa.root, abs)
		bare := ""
		if !strings.HasPrefix(cp, ".") && !filepath.IsAbs(cp) {
			bare = cp
		}
		pa.islands = append(pa.islands, &islandComp{
			bare:      bare,
			name:      name,
			src:       abs,
			rel:       filepath.ToSlash(relSrc),
			label:     where,
			framework: islandFramework(cp),
			strategy:  strategy,
			props:     structFields(full, name),
		})
	}
	return errs
}

// readIslandImports reads a .vuka file's imports of component files
// (import Counter "./islands/Counter.vue"): islands whose props the compiler
// reads from the component (an @ui.Island struct of the name wins).
func (pa *pageAssets) readIslandImports(file, rel string, line func(int) int, toks []wtok) {
	if !strings.HasSuffix(file, ".vuka") {
		return
	}
	dir := filepath.Dir(file)
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
		abs := filepath.Join(dir, filepath.FromSlash(p))
		relSrc, _ := filepath.Rel(pa.root, abs)
		ic := &islandComp{
			name:      toks[i-1].lit,
			src:       abs,
			rel:       filepath.ToSlash(relSrc),
			label:     fmt.Sprintf("%s:%d", rel, line(toks[i].off)),
			framework: islandFramework(p),
			imported:  true,
		}
		if src, err := os.ReadFile(abs); err == nil {
			if c, err := tsprops.Read(filepath.ToSlash(abs), src, func(q string) ([]byte, error) { return os.ReadFile(filepath.FromSlash(q)) }); err == nil {
				for _, pr := range c.Props {
					ic.props = append(ic.props, islandProp{name: pr.Name, ts: pr.Type.String(), optional: pr.Optional})
				}
			}
		}
		pa.islands = append(pa.islands, ic)
	}
}

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
		return parseStructBody(full, i+3)
	}
	return nil
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
	if skip {
		return nil
	}
	b, bound := reflect.StructTag(tag).Lookup("bind")
	bound = bound && b != "-"
	var out []islandProp
	for _, nm := range names {
		if !token.IsExported(nm) || nm == "Children" {
			continue
		}
		wire := nm
		if jsonName != "" {
			wire = jsonName
		}
		out = append(out, islandProp{name: wire, ts: ts, optional: optional, value: plainType(typ), bind: bound, named: localTypeName(typ)})
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
// module, minified for a build, with an inline source map in development. A
// vanilla island is dependency-free: bare (package) imports are refused.
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
		Plugins:       []api.Plugin{islandResolve()},
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

// islandResolve refuses bare imports: a vanilla island bundles its own
// relative imports only (there is no node_modules).
func islandResolve() api.Plugin {
	return api.Plugin{Name: "vuka-island", Setup: func(b api.PluginBuild) {
		b.OnResolve(api.OnResolveOptions{Filter: `^[^./]`}, func(a api.OnResolveArgs) (api.OnResolveResult, error) {
			if a.Kind == api.ResolveEntryPoint {
				return api.OnResolveResult{}, nil
			}
			return api.OnResolveResult{}, fmt.Errorf("%s is a package; a vanilla island bundles relative imports only (./x.ts) — it must be dependency-free", a.Path)
		})
	}}
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
