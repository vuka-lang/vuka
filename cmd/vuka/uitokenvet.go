package main

import (
	"bytes"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
)

// vetTokens is W0601: a vendored component (the components directory of
// [ui]) drawn with a design token — var(--ring), or a utility naming a
// colour token, bg-background — that no stylesheet of the build defines,
// so it renders unstyled. The build's stylesheets are the one holding the
// tokens ([ui] css), Tailwind's, and the local files they import.
func vetTokens(root string) []diag.Diagnostic {
	cfg, ok, err := readUIConfig(root)
	if err != nil || !ok {
		return nil
	}
	sheets := []string{filepath.Join(root, filepath.FromSlash(cfg.CSS))}
	if tw, err := tailwindFor(root); err == nil && tw != nil && tw.sheet != "" && tw.sheet != sheets[0] {
		sheets = append(sheets, tw.sheet)
	}
	declared := map[string]bool{}
	seen := map[string]bool{}
	var read func(file string)
	read = func(file string) {
		if seen[file] {
			return
		}
		seen[file] = true
		css, err := os.ReadFile(file)
		if err != nil {
			return
		}
		css = []byte(blankCSSComments(string(css)))
		for _, m := range cssDeclared.FindAllSubmatch(css, -1) {
			declared[string(m[1])] = true
		}
		for _, m := range cssLocalImport.FindAllSubmatch(css, -1) {
			if p := string(m[1]); !strings.Contains(p, ":") && (strings.HasPrefix(p, ".") || strings.HasSuffix(p, ".css")) {
				read(filepath.Join(filepath.Dir(file), filepath.FromSlash(p)))
			}
		}
	}
	for _, s := range sheets {
		read(s)
	}
	if len(seen) == 0 || !fileExists(sheets[0]) {
		return nil
	}
	lc, _ := readUILib(root)
	own := map[string]bool{} // the tokens vuka ui init writes
	if b, err := registryFS.ReadFile("registry/styles/default.css"); err == nil {
		for _, m := range cssDeclared.FindAllSubmatch(b, -1) {
			own[string(m[1])] = true
		}
	}
	dir := filepath.Join(root, filepath.FromSlash(cfg.Dir))
	type file struct {
		rel string
		src []byte
	}
	var files []file
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		n := d.Name()
		if strings.HasSuffix(n, "_test.go") || strings.HasSuffix(n, "_templ.go") || !slices.Contains([]string{".html", ".vuka", ".templ", ".go", ".js"}, filepath.Ext(n)) {
			return nil
		}
		if src, err := os.ReadFile(p); err == nil {
			rel, _ := filepath.Rel(root, p)
			files = append(files, file{filepath.ToSlash(rel), src})
		}
		return nil
	})
	// A custom property a component sets itself (style="--x: …", a
	// script's setProperty) is defined.
	for _, f := range files {
		for _, m := range cssDeclared.FindAllSubmatch(f.src, -1) {
			declared[string(m[1])] = true
		}
		for _, m := range quotedProp.FindAllSubmatch(f.src, -1) {
			declared[string(m[1])] = true
		}
	}
	var colours []string
	for n := range declared {
		if c, ok := strings.CutPrefix(n, "--color-"); ok {
			colours = append(colours, c)
		}
	}
	slices.Sort(colours)
	defined := slices.Sorted(mapKeys(declared))

	var out []diag.Diagnostic
	for _, f := range files {
		if filepath.Ext(f.rel) == ".js" {
			continue
		}
		expected := cfg.CSS + " (the design tokens)"
		inDir := strings.TrimPrefix(f.rel, cfg.Dir+"/")
		sub, rest, nested := strings.Cut(inDir, "/")
		lib := ""
		if s, ok := lc.sources[sub]; ok && nested && rest != "" {
			lib = libOf(s)
		}
		component := strings.TrimSuffix(strings.TrimSuffix(path.Base(f.rel), path.Ext(f.rel)), ".vuka")
		if nested {
			component = sub
		}
		src := f.src
		if ext := filepath.Ext(f.rel); ext == ".go" || ext == ".templ" {
			src = blankGoComments(src)
		}
		lines := lineStarts(src)
		reported := map[string]bool{}
		report := func(off int, used, token, near string) {
			if reported[used] {
				return
			}
			reported[used] = true
			sheet := expected
			if lib != "" && !own[token] {
				sheet = path.Join(path.Dir(cfg.CSS), lib+".css") + " (" + lib + "'s theme)"
			}
			l, _ := slices.BinarySearch(lines, off+1)
			col := off - lines[l-1] + 1
			d := diag.Warn("W0601", "%s uses %s, which no stylesheet of the build defines: %s was expected to define %s", component, used, sheet, token).
				At(diag.Span{File: f.rel, Line: l, Col: col, EndCol: col + len(used)}).
				WithHelp("define %s in %s, or use a token it defines", token, strings.Fields(sheet)[0])
			if near != "" {
				d.WithSuggest(near)
			}
			out = append(out, *d)
		}
		for _, m := range varRef.FindAllSubmatchIndex(src, -1) {
			name := string(src[m[2]:m[3]])
			if declared[name] || tailwindVar(name) {
				continue
			}
			report(m[2], name, name, closest(name, defined))
		}
		for _, m := range quotedSrc.FindAllSubmatchIndex(src, -1) {
			if m[2] < 0 {
				m[2], m[3] = m[4], m[5]
			}
			s := src[m[2]:m[3]]
			// One word is a class list in a class attribute; elsewhere it
			// may be any name (an icon's, a key).
			if !classList.Match(s) || len(strings.Fields(string(s))) < 2 && !classAttr.Match(src[:m[0]]) {
				continue
			}
			for _, w := range classWord.FindAllIndex(s, -1) {
				c := classToken.FindSubmatchIndex(s[w[0]:w[1]])
				if c == nil {
					continue
				}
				at := m[2] + w[0] + c[2] // the utility, after its variants
				utility, full := string(s[w[0]+c[2]:w[0]+c[3]]), string(s[w[0]+c[4]:w[0]+c[5]])
				if v, ok := strings.CutPrefix(full, "("); ok {
					name := strings.TrimSuffix(v, ")")
					if !declared[name] && !tailwindVar(name) {
						report(at+len(utility)+2, name, name, closest(name, defined))
					}
					continue
				}
				name, _, _ := strings.Cut(full, "/")
				if !colourName(name) || declared["--color-"+name] || declared["--text-"+name] || tailwindColour(name) {
					continue
				}
				near := ""
				if n := closest(name, colours); n != "" {
					near = utility + "-" + n
				}
				report(at, utility+"-"+name, "--color-"+name, near)
			}
		}
	}
	return out
}

var (
	cssLocalImport = regexp.MustCompile(`@import\s+(?:url\()?["']([^"']+)["']`)
	classAttr      = regexp.MustCompile(`(?i)\bclass\s*[=:]\s*\{?\s*$`)
	quotedProp     = regexp.MustCompile(`["'](--[\w-]+)["']`)
	varRef         = regexp.MustCompile(`var\(\s*(--[\w-]+)\s*\)`)
	quotedSrc      = regexp.MustCompile("\"([^\"\\n]*)\"|`([^`]*)`")
	classList      = regexp.MustCompile(`^\s*[\w:/.\[\]()%#!&*>=,-]+(?:\s+[\w:/.\[\]()%#!&*>=,-]+)*\s*$`)
	classWord      = regexp.MustCompile(`\S+`)
	classToken     = regexp.MustCompile(`^!?(?:[^:\s]+:)*!?(border-[xytrblse]|ring-offset|bg|text|border|ring|outline|fill|stroke|divide|accent|caret|decoration|placeholder)-(\(--[\w-]+\)|[a-z][\w-]*(?:/[\w.]+)?)!?$`)
)

// colourName is whether a utility's name could be a colour token's, not a
// size, side, style or keyword of the utilities sharing its prefix.
func colourName(name string) bool {
	first, _, _ := strings.Cut(name, "-")
	return !nonColour[first] && !strings.ContainsAny(first, "0123456789")
}

var nonColour = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`xs sm base md lg xl left center right justify start end wrap nowrap balance pretty clip
		ellipsis solid dashed dotted double hidden none collapse separate inset auto x y t r b l s e tl tr bl br ss se es ee
		cover contain fixed local scroll repeat no round space top bottom origin blend linear radial conic gradient size
		position wavy offset inner opacity underline overline line through normal italic truncate upper lower capitalize
		indent all current inherit transparent black white initial shadow from via to dark light mode
		color align overflow style width family weight spacing shadow image attachment repeat`) {
		nonColour[w] = true
	}
}

// tailwindPalette are the colours Tailwind's own theme defines, each with
// its shades.
var tailwindPalette = strings.Fields(`slate gray zinc neutral stone red orange amber yellow lime green emerald teal cyan
	sky blue indigo violet purple fuchsia pink rose mauve olive mist taupe`)

// tailwindColour is whether Tailwind's theme defines the colour name.
func tailwindColour(name string) bool {
	base, shade, ok := strings.Cut(name, "-")
	if !ok || !slices.Contains(tailwindPalette, base) {
		return false
	}
	_, err := strconv.Atoi(shade)
	return err == nil
}

// tailwindVar is whether Tailwind defines the custom property: its own
// (--tw-…), or one of its default theme's.
func tailwindVar(name string) bool {
	if c, ok := strings.CutPrefix(name, "--color-"); ok {
		return tailwindColour(c) || c == "black" || c == "white"
	}
	for _, p := range []string{"--tw-", "--spacing", "--font-", "--text-", "--tracking-", "--leading-", "--breakpoint-", "--container-",
		"--shadow-", "--inset-shadow-", "--drop-shadow-", "--text-shadow-", "--blur-", "--perspective-", "--aspect-", "--ease-",
		"--animate-", "--default-", "--radius-"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// blankGoComments is Go (or templ) source with its comments spaces.
func blankGoComments(src []byte) []byte {
	b := slices.Clone(src)
	var quote byte
	for i := 0; i < len(b); i++ {
		switch c := b[i]; {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote || c == '\n' && quote == '"' {
				quote = 0
			}
		case c == '"' || c == '`':
			quote = c
		case c == '/' && i+1 < len(b) && (b[i+1] == '/' || b[i+1] == '*'):
			end := len(b)
			if b[i+1] == '/' {
				if n := bytes.IndexByte(b[i:], '\n'); n >= 0 {
					end = i + n
				}
			} else if n := bytes.Index(b[i+2:], []byte("*/")); n >= 0 {
				end = i + 2 + n + 2
			}
			for j := i; j < end; j++ {
				if b[j] != '\n' {
					b[j] = ' '
				}
			}
			i = end - 1
		}
	}
	return b
}

// lineStarts are the offsets the lines of src start at.
func lineStarts(src []byte) []int {
	out := []int{0}
	for i, c := range src {
		if c == '\n' {
			out = append(out, i+1)
		}
	}
	return out
}
