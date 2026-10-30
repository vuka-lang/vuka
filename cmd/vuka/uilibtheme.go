package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// A library's theme in the app: its stylesheet styles/<library>.css, which
// vuka ui add writes (again as components are added) and the app's
// stylesheet imports. It holds the theme's variables the app's tokens
// don't declare, dark from the visitor's preference as the tokens are
// (never a .dark class), the stylesheets the components need (copied
// beside it), and the rules of the library's style for the classes the
// app's components of it use.

// libTheme queues the library's stylesheets on out, the import of its own
// in the app's stylesheet, and the layout's colours from the tokens.
func libTheme(out *uiWriter, cfg uiConfig, src *libSource, lc *uiLibConfig, force bool) error {
	spec := src.spec
	if spec.Theme == "" && spec.Style == "" && len(spec.CSS) == 0 {
		return nil
	}
	cssDir := path.Dir(cfg.CSS)
	appCSS, err := out.read(cfg.CSS)
	if err != nil {
		return err
	}
	var body bytes.Buffer
	fmt.Fprintf(&body, "/* %s's theme and styles, from %s (vuka ui add writes this file\n   again as components are added: change the tokens in %s instead). */\n", spec.Name, src.where(), path.Base(cfg.CSS))
	for _, c := range spec.CSS {
		b, err := os.ReadFile(filepath.Join(src.dir, filepath.FromSlash(c)))
		if err != nil {
			return fmt.Errorf("vuka ui add: %s's stylesheet %s: %v", spec.Name, c, err)
		}
		name := path.Base(c)
		rel := path.Join(cssDir, name)
		b = classDark.ReplaceAll(b, nil)
		switch have, err := out.read(rel); {
		case err != nil:
			return err
		case have == nil || force:
			out.write(rel, b, "")
		case !bytes.Equal(have, b):
			out.note("%s is yours, differing from %s's", rel, src.label())
		}
		fmt.Fprintf(&body, "@import %q;\n", "./"+name)
	}
	if spec.Theme != "" {
		b, err := os.ReadFile(filepath.Join(src.dir, filepath.FromSlash(spec.Theme)))
		if err != nil {
			return fmt.Errorf("vuka ui add: %s's theme %s: %v", spec.Name, spec.Theme, err)
		}
		body.WriteString(themeAdditions(string(b), string(appCSS)))
	}
	if spec.Style != "" {
		b, err := os.ReadFile(filepath.Join(src.dir, filepath.FromSlash(spec.Style)))
		if err != nil {
			return fmt.Errorf("vuka ui add: %s's style %s: %v", spec.Name, spec.Style, err)
		}
		used, err := libWords(out, cfg, lc, spec.Name)
		if err != nil {
			return err
		}
		if rules := styleRules(string(b), used); rules != "" {
			fmt.Fprintf(&body, "\n/* The styles of the components' classes. */\n@layer base {\n%s}\n", rules)
		}
	}
	own := path.Join(cssDir, spec.Name+".css")
	if have, _ := out.read(own); !bytes.Equal(have, body.Bytes()) {
		out.write(own, body.Bytes(), spec.Name+"'s theme and styles")
	}
	imp := fmt.Sprintf("@import %q;\n", "./"+spec.Name+".css")
	if !bytes.Contains(appCSS, []byte(strings.TrimSuffix(imp, "\n"))) {
		if !twImport.Match(appCSS) {
			appCSS = append([]byte("@import \"tailwindcss\";\n"), appCSS...)
		}
		at := len(cssImports.Find(appCSS))
		out.write(cfg.CSS, slices.Concat(appCSS[:at], []byte(imp), appCSS[at:]), "imports "+spec.Name+".css")
	}
	return themedLayout(out)
}

var (
	classDark = regexp.MustCompile(`(?m)^[ \t]*@custom-variant\s+dark\b[^;\n]*;?[ \t]*\n?`)
	cssWord   = regexp.MustCompile(`[A-Za-z_][\w-]*`)
)

// libWords are the words of the app's files of the library's components
// (those queued on out among them): the classes they may use.
func libWords(out *uiWriter, cfg uiConfig, lc *uiLibConfig, lib string) (map[string]bool, error) {
	words := map[string]bool{}
	add := func(b []byte) {
		for _, w := range cssWord.FindAll(b, -1) {
			words[string(w)] = true
		}
	}
	for dir, s := range lc.sources {
		if libOf(s) != lib {
			continue
		}
		prefix := path.Join(cfg.Dir, dir) + "/"
		entries, _ := os.ReadDir(filepath.Join(out.root, filepath.FromSlash(prefix)))
		for _, e := range entries {
			if ext := filepath.Ext(e.Name()); ext == ".templ" || ext == ".go" {
				b, err := out.read(prefix + e.Name())
				if err != nil {
					return nil, err
				}
				add(b)
			}
		}
		for _, rel := range out.files {
			if ext := filepath.Ext(rel); strings.HasPrefix(rel, prefix) && (ext == ".templ" || ext == ".go") {
				add(out.data[rel])
			}
		}
	}
	return words, nil
}

// read is a file as out leaves it: what it is to write, else the app's;
// nil when neither has it.
func (u *uiWriter) read(rel string) ([]byte, error) {
	if b, ok := u.data[rel]; ok {
		return b, nil
	}
	b, err := os.ReadFile(filepath.Join(u.root, filepath.FromSlash(rel)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// where is the source as a stylesheet's comment names it.
func (s *libSource) where() string {
	if s.version == "" {
		return s.spec.Module + " (" + s.spec.Dir + ")"
	}
	return s.spec.Module + "@" + s.version
}

// cssRule is a rule of a stylesheet: its prelude (selector, or at-rule)
// and its block, and the text of both.
type cssRule struct {
	prelude, body, text string
}

// cssRules are the top-level rules of css, its comments dropped.
func cssRules(css string) []cssRule {
	css = blankCSSComments(css)
	var out []cssRule
	start, depth, open := 0, 0, 0
	var quote byte
	for i := 0; i < len(css); i++ {
		c := css[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '{':
			if depth == 0 {
				open = i
			}
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				out = append(out, cssRule{strings.TrimSpace(css[start:open]), css[open+1 : i], strings.TrimSpace(css[start : i+1])})
				start = i + 1
			}
		case c == ';' && depth == 0:
			start = i + 1 // a statement: @import, @custom-variant …;
		}
	}
	return out
}

// blankCSSComments is css with its comments spaces.
func blankCSSComments(css string) string {
	b := []byte(css)
	for i := 0; i+1 < len(b); i++ {
		if b[i] == '/' && b[i+1] == '*' {
			end := bytes.Index(b[i+2:], []byte("*/"))
			if end < 0 {
				end = len(b) - i - 2
			}
			for j := i; j < i+2+end+2 && j < len(b); j++ {
				if b[j] != '\n' {
					b[j] = ' '
				}
			}
		}
	}
	return string(b)
}

// cssDecl is a custom property's declaration.
type cssDecl struct{ name, value string }

var cssCustomProp = regexp.MustCompile(`^\s*(--[\w-]+)\s*:\s*([\s\S]*?)\s*$`)

// customProps are the custom properties a block declares, in order.
func customProps(body string) []cssDecl {
	var out []cssDecl
	depth, start := 0, 0
	for i := 0; i <= len(body); i++ {
		if i < len(body) {
			switch body[i] {
			case '{':
				depth++
				continue
			case '}':
				depth--
				if depth == 0 {
					start = i + 1
				}
				continue
			case ';':
			default:
				continue
			}
		}
		if depth == 0 {
			if m := cssCustomProp.FindStringSubmatch(body[start:i]); m != nil {
				out = append(out, cssDecl{m[1], m[2]})
			}
			start = i + 1
		}
	}
	return out
}

var (
	cssDeclared = regexp.MustCompile(`(--[\w-]+)\s*:`)
	cssVarRef   = regexp.MustCompile(`var\(\s*(--[\w-]+)`)
)

// themeAdditions are the variables of a library's theme the app's
// stylesheet doesn't declare: its @theme inline entries (not those naming
// themselves, a font the theme's site loads), and the :root and .dark
// values those use, dark as the tokens are, from the visitor's preference
// unless data-theme says otherwise.
func themeAdditions(theme, app string) string {
	declared := map[string]bool{}
	for _, m := range cssDeclared.FindAllStringSubmatch(blankCSSComments(app), -1) {
		declared[m[1]] = true
	}
	var tokens, root, dark []cssDecl
	for _, r := range cssRules(theme) {
		switch strings.Join(strings.Fields(r.prelude), " ") {
		case "@theme inline", "@theme":
			tokens = append(tokens, customProps(r.body)...)
		case ":root":
			root = append(root, customProps(r.body)...)
		case ".dark", ":root.dark", "html.dark":
			dark = append(dark, customProps(r.body)...)
		}
	}
	rootOf := map[string]string{}
	for _, d := range root {
		rootOf[d.name] = d.value
	}
	var addTokens []cssDecl
	need := map[string]bool{}
	var want func(v string)
	want = func(v string) {
		for _, m := range cssVarRef.FindAllStringSubmatch(v, -1) {
			if n := m[1]; !declared[n] && !need[n] {
				if _, ok := rootOf[n]; ok {
					need[n] = true
					want(rootOf[n])
				}
			}
		}
	}
	for _, d := range tokens {
		if declared[d.name] || strings.TrimSpace(d.value) == "var("+d.name+")" {
			continue
		}
		addTokens = append(addTokens, d)
		declared[d.name] = true
		want(d.value)
	}
	if len(addTokens) == 0 {
		return ""
	}
	block := func(b *strings.Builder, open, indent string, ds []cssDecl, keep func(string) bool) {
		var lines []string
		for _, d := range ds {
			if keep(d.name) {
				lines = append(lines, fmt.Sprintf("%s  %s: %s;\n", indent, d.name, d.value))
			}
		}
		if len(lines) == 0 {
			return
		}
		fmt.Fprintf(b, "%s%s {\n%s%s}\n", indent, open, strings.Join(lines, ""), indent)
	}
	var b strings.Builder
	b.WriteString("\n/* The theme's tokens the app's don't have. */\n")
	block(&b, "@theme inline", "", addTokens, func(string) bool { return true })
	block(&b, ":root", "", root, func(n string) bool { return need[n] })
	var darkB strings.Builder
	block(&darkB, `:root:not([data-theme="light"])`, "  ", dark, func(n string) bool { return need[n] })
	if darkB.Len() > 0 {
		fmt.Fprintf(&b, "@media (prefers-color-scheme: dark) {\n%s}\n", darkB.String())
		block(&b, `:root[data-theme="dark"]`, "", dark, func(n string) bool { return need[n] })
	}
	return b.String()
}

var cssClass = regexp.MustCompile(`\.([A-Za-z_][\w-]*)`)

// styleRules are the rules of a library's style for the classes used: a
// style scoped to one class wrapping all of its rules (.style-x { … }) is
// unwrapped; a rule is kept when a class of its selector is a word of
// used. Indented two spaces, for @layer base.
func styleRules(style string, used map[string]bool) string {
	rules := cssRules(style)
	if len(rules) == 1 && cssClass.MatchString(rules[0].prelude) && cssClass.FindString(rules[0].prelude) == rules[0].prelude {
		rules = cssRules(rules[0].body)
	}
	// A word ending in - is a class's start, the rest made at run time
	// ("wd-button-" + variant).
	var prefixes []string
	for w := range used {
		if strings.HasSuffix(w, "-") && strings.Count(w, "-") > 1 {
			prefixes = append(prefixes, w)
		}
	}
	var b strings.Builder
	for _, r := range rules {
		keep := false
		for _, m := range cssClass.FindAllStringSubmatch(r.prelude, -1) {
			keep = keep || used[m[1]] || slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(m[1], p) })
		}
		if !keep {
			continue
		}
		for _, l := range strings.Split(dedentRule(r.text), "\n") {
			if strings.TrimSpace(l) == "" {
				continue
			}
			b.WriteString("  " + l + "\n")
		}
	}
	return b.String()
}

// dedentRule is text with the indentation its lines after the first share
// removed from them.
func dedentRule(text string) string {
	lines := strings.Split(text, "\n")
	common := -1
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " \t"))
		if common < 0 || n < common {
			common = n
		}
	}
	// The closing brace sits one level out from the declarations.
	if last := lines[len(lines)-1]; strings.TrimSpace(last) == "}" {
		common = len(last) - len(strings.TrimLeft(last, " \t"))
	}
	if common <= 0 {
		return text
	}
	for i := 1; i < len(lines); i++ {
		if len(lines[i]) >= common && strings.TrimSpace(lines[i][:common]) == "" {
			lines[i] = lines[i][common:]
		}
	}
	return strings.Join(lines, "\n")
}

// The colours vuka new's layouts give the document, and the tokens'.
var layoutColours = []struct{ file, from, to string }{
	{"pages/_layout.vuka.html", `class="h-full bg-slate-50 antialiased dark:bg-slate-950"`, `class="h-full bg-background text-foreground antialiased"`},
	{"pages/_layout.vuka.html", `class="h-full font-sans text-slate-900 dark:text-slate-100"`, `class="h-full font-sans"`},
	{"layouts/layouts.templ", `class="h-full bg-slate-50 antialiased dark:bg-slate-950"`, `class="h-full bg-background text-foreground antialiased"`},
	{"layouts/layouts.templ", `class="h-full font-sans text-slate-900 dark:text-slate-100"`, `class="h-full font-sans"`},
}

// themedLayout draws the document of a layout vuka new wrote with the
// tokens' colours, as the library's components are, in both themes.
func themedLayout(out *uiWriter) error {
	for _, f := range []string{"pages/_layout.vuka.html", "layouts/layouts.templ"} {
		b, err := out.read(f)
		if err != nil || b == nil {
			continue
		}
		next := b
		for _, c := range layoutColours {
			if c.file == f {
				next = bytes.Replace(next, []byte(c.from), []byte(c.to), 1)
			}
		}
		if !bytes.Equal(next, b) {
			out.write(f, next, "the document's colours from the tokens (bg-background, text-foreground)")
		}
	}
	return nil
}
