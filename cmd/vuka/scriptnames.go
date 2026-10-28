package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// The names a script passes as strings, read from the Go and the markup
// so vuka-component.d.ts can type them: ctx.ref's (the ref="…" of the
// component's markup), ctx.on's (the PushEvent calls of its methods, with
// their payloads' types), ctx.emit's (the on… attributes of its markup's
// root, where the loader finds the handler a parent's tag gave), a page's
// @ui.URL keys (patch({q: …})) and the module's folder routes (navigate).

// scriptNames are what a component's script may name.
type scriptNames struct {
	refs        []scriptRef // in the order the markup has them
	dynamicRefs bool        // a ref={…} whose name the markup doesn't spell
	emits       []string
	markup      bool // the markup was read: refs and emits are all there are
	events      []scriptEvent
	dynamicEvts bool // a PushEvent whose name isn't a string literal
	urlKeys     []string
}

type scriptRef struct {
	name, tag         string
	conditional, loop bool
}

type scriptEvent struct {
	name    string
	payload []wtok // the payload's Go type; nil when it can't be read
	ts      string // or its TypeScript, for a literal
	scope   *tsScope
}

// markupOf is the markup of the shape of type name in the template files
// files: the text after its Go, its scripts and styles cut.
func markupOf(files []string, name string) (string, bool) {
	for _, f := range files {
		src, err := load.ReadSource(f)
		if err != nil {
			continue
		}
		sh, _ := transpile.ParseShape(transpile.HeaderView(src))
		if sh == nil || sh.Type != name {
			continue
		}
		lines := strings.SplitAfter(string(src), "\n")
		if sh.Markup >= len(lines) {
			continue
		}
		markup := strings.Join(lines[sh.Markup:], "")
		for _, a := range transpile.TemplateAssets(filepath.Base(f), src) {
			if a.Decl == name {
				markup = strings.Replace(markup, a.Text, "", 1)
			}
		}
		return markup, true
	}
	return "", false
}

// startTag is an element's start tag: its name and attributes, a value
// as written (quoted, or {…}).
type startTag struct {
	name  string
	attrs [][2]string
}

// startTags are the start tags of markup, in order. A tag ends at a >
// outside quotes and braces; Go between the tags is skipped.
func startTags(markup string) []startTag {
	var out []startTag
	for i := 0; i < len(markup); i++ {
		if markup[i] != '<' || i+1 >= len(markup) || !isLetter(markup[i+1]) {
			continue
		}
		j := i + 1
		for j < len(markup) && (isIdentByte(markup[j]) || strings.IndexByte(".:-", markup[j]) >= 0) {
			j++
		}
		t := startTag{name: markup[i+1 : j]}
		for j < len(markup) && markup[j] != '>' {
			for j < len(markup) && (markup[j] == ' ' || markup[j] == '\t' || markup[j] == '\n' || markup[j] == '\r' || markup[j] == '/') {
				j++
			}
			if j >= len(markup) || markup[j] == '>' {
				break
			}
			start := j
			if markup[j] == '{' {
				j = skipBraces(markup, j)
				continue // {spread...}
			}
			for j < len(markup) && !strings.ContainsRune(" \t\r\n=>/", rune(markup[j])) {
				j++
			}
			name := markup[start:j]
			val := ""
			if j < len(markup) && markup[j] == '=' {
				j++
				vs := j
				switch {
				case j < len(markup) && (markup[j] == '"' || markup[j] == '\''):
					q := markup[j]
					j++
					for j < len(markup) && markup[j] != q {
						j++
					}
					j++
				case j < len(markup) && markup[j] == '{':
					j = skipBraces(markup, j)
				default:
					for j < len(markup) && !strings.ContainsRune(" \t\r\n>", rune(markup[j])) {
						j++
					}
				}
				val = markup[vs:min(j, len(markup))]
			}
			if name == "" && j == start {
				j++
				continue
			}
			if name != "" {
				t.attrs = append(t.attrs, [2]string{name, val})
			}
		}
		out = append(out, t)
		i = j
	}
	return out
}

// skipBraces is the offset after the } closing the { at i, strings in it
// skipped.
func skipBraces(s string, i int) int {
	depth := 0
	for ; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return i + 1
			}
		case '"', '\'', '`':
			q := s[i]
			for i++; i < len(s) && s[i] != q; i++ {
				if s[i] == '\\' && q != '`' {
					i++
				}
			}
		}
	}
	return i
}

// readMarkupNames fills n's refs and emits from markup.
func (n *scriptNames) readMarkupNames(markup string) {
	n.markup = true
	positions := transpile.TemplateRefs(markup)
	tags := startTags(markup)
	for k, t := range tags {
		for _, a := range t.attrs {
			switch {
			case a[0] == "ref":
				name, err := strconv.Unquote(`"` + strings.Trim(a[1], `"'`) + `"`)
				if err != nil || strings.HasPrefix(a[1], "{") || name == "" || strings.ContainsAny(name, "{}") {
					n.dynamicRefs = true
					continue
				}
				conditional, loop := false, false
				for _, r := range positions {
					if r.Name == name {
						conditional = conditional || r.Conditional
						loop = loop || r.Loop
					}
				}
				if !slices.ContainsFunc(n.refs, func(r scriptRef) bool { return r.name == name }) {
					n.refs = append(n.refs, scriptRef{name: name, tag: t.name, conditional: conditional, loop: loop})
				}
			case k == 0 && len(a[0]) > 2 && strings.HasPrefix(a[0], "on") && isUpperByte(a[0][2]):
				// The root's on… attribute: data-vk-on-<event>, which
				// ctx.emit("<event>") finds.
				if e := strings.ToLower(a[0][2:]); !slices.Contains(n.emits, e) {
					n.emits = append(n.emits, e)
				}
			}
		}
	}
}

func isUpperByte(b byte) bool { return b >= 'A' && b <= 'Z' }

// pushEventCall finds a PushEvent call: its arguments follow.
var pushEventCall = regexp.MustCompile(`\.PushEvent\(`)

// readGoNames fills n's events and URL keys from the Go of the package in
// dir: the PushEvent calls in the methods of type name, and its fields'
// @ui.URL keys.
func (n *scriptNames) readGoNames(r *tsTypes, dir, name string) {
	entries, _ := load.ReadDir(dir)
	recv := regexp.MustCompile(`(?m)^func\s*\(\s*\w*\s*\*?\s*` + regexp.QuoteMeta(name) + `(?:\[[^\]]*\])?\s*\)\s*\w+[^{]*\{`)
	decl := regexp.MustCompile(`(?m)^type\s+` + regexp.QuoteMeta(name) + `(?:\[[^\]]*\])?\s+struct\s*\{`)
	for _, e := range entries {
		file := e.Name()
		if e.IsDir() || strings.Contains(file, "_test.") || strings.HasSuffix(file, "_vuka.go") || strings.HasSuffix(file, "_html.go") {
			continue
		}
		src, ok := goSource(filepath.Join(dir, file))
		if !ok {
			continue
		}
		text := string(src)
		full, _ := scanFile(file, src)
		scope := &tsScope{pkg: r.pkg(dir), imports: fileImports(dropNewlineSemis(full))}
		for _, m := range recv.FindAllStringIndex(text, -1) {
			end := goEndBrace(text, m[1])
			if end < 0 {
				continue
			}
			body := text[m[1]:end]
			for _, c := range pushEventCall.FindAllStringIndex(body, -1) {
				args := callArgText(body[c[1]:])
				if len(args) == 0 {
					continue
				}
				ev, err := strconv.Unquote(strings.TrimSpace(args[0]))
				if err != nil {
					n.dynamicEvts = true
					continue
				}
				if slices.ContainsFunc(n.events, func(x scriptEvent) bool { return x.name == ev }) {
					continue
				}
				se := scriptEvent{name: ev, scope: scope, ts: "unknown"}
				if len(args) > 1 {
					se.payload, se.ts = payloadType(args[1])
				}
				n.events = append(n.events, se)
			}
		}
		if loc := decl.FindStringIndex(text); loc != nil {
			if end := goEndBrace(text, loc[1]); end > 0 {
				for _, m := range urlKeyAttr.FindAllStringSubmatch(text[loc[1]:end], -1) {
					if !slices.Contains(n.urlKeys, m[1]) {
						n.urlKeys = append(n.urlKeys, m[1])
					}
				}
			}
		}
	}
}

// urlKeyAttr is a field's @ui.URL("key").
var urlKeyAttr = regexp.MustCompile(`@\w+\.URL\(\s*"([^"]+)"`)

// callArgText are the arguments of the call whose ( src follows, as written.
func callArgText(src string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				if s := strings.TrimSpace(src[start:i]); s != "" {
					out = append(out, s)
				}
				return out
			}
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(src[start:i]))
				start = i + 1
			}
		case '"', '\'', '`':
			q := src[i]
			for i++; i < len(src) && src[i] != q; i++ {
				if src[i] == '\\' && q != '`' {
					i++
				}
			}
		}
	}
	return nil
}

// payloadType is the Go type of a PushEvent payload as written — a
// composite literal's type — or its TypeScript for a literal: unknown when
// neither says.
func payloadType(expr string) ([]wtok, string) {
	x, err := parser.ParseExpr(expr)
	if err != nil {
		return nil, "unknown"
	}
	if u, ok := x.(*ast.UnaryExpr); ok && u.Op == token.AND {
		x = u.X
	}
	switch x := x.(type) {
	case *ast.CompositeLit:
		if x.Type == nil {
			return nil, "unknown"
		}
		typ := expr[x.Type.Pos()-1 : x.Type.End()-1]
		full, _ := scanFile("", []byte(typ))
		return dropNewlineSemis(full), ""
	case *ast.BasicLit:
		switch x.Kind {
		case token.STRING, token.CHAR:
			return nil, "string"
		default:
			return nil, "number"
		}
	case *ast.Ident:
		switch x.Name {
		case "true", "false":
			return nil, "boolean"
		case "nil":
			return nil, "null"
		}
	}
	return nil, "unknown"
}

// routePatterns are the URL patterns of the module's folder-route pages, a
// parameter as ${string}: "/users/${string}".
func routePatterns(root, modPath string) []string {
	var out []string
	for _, t := range scanRoutes(root, modPath, nil) {
		for _, d := range t.dirs {
			for _, p := range d.pages {
				var segs []string
				for _, s := range strings.Split(urlRel(d.rel), "/") {
					if s != "" {
						segs = append(segs, s)
					}
				}
				if f := d.fileOf[p]; f != "" && fileSeg(f) != "" {
					segs = append(segs, fileSeg(f))
				} else if b := d.bracket[p]; b != "" && !slices.Contains(segs, b) {
					segs = append(segs, b)
				}
				for i, s := range segs {
					if load.Bracketed(s) {
						segs[i] = "${string}"
					}
				}
				pat := "/" + strings.Join(segs, "/")
				if !slices.Contains(out, pat) {
					out = append(out, pat)
				}
			}
		}
	}
	slices.Sort(out)
	return out
}

// setupRefNote adds a shape-specific spelling or collection hint to a ref error.
func setupRefNote(text string, refs []scriptRef, off int) string {
	for local, kind := range setupAliases(text) {
		if kind != "ref" {
			continue
		}
		call := regexp.MustCompile(`\b` + regexp.QuoteMeta(local) + `(\.all)?\(\s*(['"])([^'"\n]*)['"]`)
		for _, m := range call.FindAllStringSubmatchIndex(blankComments(text), -1) {
			if m[0] > 0 && text[m[0]-1] == '.' || off < m[0] || off >= m[1] {
				continue
			}
			name := text[m[6]:m[7]]
			var names []string
			for _, r := range refs {
				names = append(names, r.name)
				if r.name == name {
					if r.loop && m[2] < 0 {
						return "Use " + local + ".all('" + name + "') for a ref inside a for."
					}
					return ""
				}
			}
			if near := closest(name, names); near != "" {
				return "Did you mean " + strconv.Quote(near) + "?"
			}
		}
	}
	return ""
}
