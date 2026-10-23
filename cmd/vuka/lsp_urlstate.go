package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The editor's view of URL state: hover on a field's @ui.URL names the
// parameter it is kept in, and completion inside @ui.URL("…") offers the
// keys the package's other fields keep.

// urlAttrRe is a field's @ui.URL attribute: bare, with its key, and with
// a default.
var urlAttrRe = regexp.MustCompile(`@ui\.URL(?:\(\s*("(?:[^"\\]|\\.)*")?\s*\))?(?:\.Default\(([^()]*)\))?`)

// urlFieldRe is the field a line declares before its attributes: its name.
var urlFieldRe = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s`)

// urlAttr is one @ui.URL attribute in a file.
type urlAttr struct {
	start, end int
	field      string
	key        string // "" for a bare @ui.URL
	bare       bool
	def        string
}

// urlAttrs are the @ui.URL attributes of text, each after a field.
func urlAttrs(text []byte) []urlAttr {
	if !bytes.Contains(text, []byte("@ui.URL")) {
		return nil
	}
	var out []urlAttr
	for _, m := range urlAttrRe.FindAllSubmatchIndex(text, -1) {
		if m[1] < len(text) && isIdentByte(text[m[1]]) {
			continue // @ui.URLx: another name
		}
		ls := bytes.LastIndexByte(text[:m[0]], '\n') + 1
		fm := urlFieldRe.FindSubmatch(text[ls:m[0]])
		if fm == nil {
			continue
		}
		a := urlAttr{start: m[0], end: m[1], field: string(fm[1])}
		switch {
		case m[2] >= 0:
			a.key, _ = strconv.Unquote(string(text[m[2]:m[3]]))
		default:
			a.bare = !bytes.HasPrefix(text[m[0]:m[1]], []byte(`@ui.URL(`))
			a.bare = a.bare || bytes.HasPrefix(text[m[0]:m[1]], []byte(`@ui.URL()`))
		}
		if m[4] >= 0 {
			a.def = strings.TrimSpace(string(text[m[4]:m[5]]))
		}
		out = append(out, a)
	}
	return out
}

// urlHover is what hover says of a @ui.URL attribute.
func urlHover(a urlAttr) string {
	var b strings.Builder
	if a.bare {
		fmt.Fprintf(&b, "`%s` binds the route's path parameter `{%s}` (matched ignoring case), read when the component renders and whenever the page's URL changes. A path parameter is read only: move to another page to change it.", a.field, a.field)
		return b.String()
	}
	fmt.Fprintf(&b, "`%s` is kept in the URL as `?%s=`: filled from it as the component renders and whenever the URL changes; an event assigning it patches the page's URL, the other parameters kept.", a.field, a.key)
	if a.def != "" {
		fmt.Fprintf(&b, "\n\nWith no parameter it is `%s`, and holding `%s` leaves the parameter out.", a.def, a.def)
	} else {
		b.WriteString("\n\nWith no parameter it is the zero value, and holding it leaves the parameter out.")
	}
	fmt.Fprintf(&b, "\n\nUnder a tag's `urlKey=\"name\"` the parameter is `?name.%s=`. A prop the parent passes wins over the URL.", a.key)
	return b.String()
}

// urlKeyAt is the @ui.URL("…") string off is inside of, as its offsets:
// the text typed so far starts at from.
func urlKeyAt(text []byte, off int) (from int, ok bool) {
	ls := bytes.LastIndexByte(text[:off], '\n') + 1
	line := text[ls:off]
	i := bytes.LastIndex(line, []byte(`@ui.URL("`))
	if i < 0 {
		return 0, false
	}
	rest := line[i+len(`@ui.URL("`):]
	if bytes.ContainsAny(rest, `"`) {
		return 0, false
	}
	return ls + i + len(`@ui.URL("`), true
}

// packageURLKeys are the query keys the @ui.URL attributes of the .vuka and
// .vuka.html files in dir keep, with the fields keeping them; text stands
// for the file at path, which may be unsaved.
func packageURLKeys(dir, path string, text []byte) map[string][]string {
	keys := map[string][]string{}
	add := func(src []byte) {
		for _, a := range urlAttrs(src) {
			if !a.bare && a.key != "" && !containsStr(keys[a.key], a.field) {
				keys[a.key] = append(keys[a.key], a.field)
			}
		}
	}
	add(text)
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if filepath.Join(dir, n) == path || !(strings.HasSuffix(n, ".vuka") || strings.HasSuffix(n, ".vuka.html")) {
			continue
		}
		if b, err := os.ReadFile(filepath.Join(dir, n)); err == nil {
			add(b)
		}
	}
	return keys
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// urlRequest answers hover on a field's @ui.URL attribute and completion
// of the key in @ui.URL("…"); nil when off is at neither. p.mu must be
// held.
func (p *proxy) urlRequest(method string, f *vfile, off int) func() any {
	text := f.text()
	switch method {
	case "textDocument/hover":
		for _, a := range urlAttrs(text) {
			if off >= a.start && off <= a.end {
				doc := urlHover(a)
				return func() any { return markdownHover(doc, text, a.start, a.end) }
			}
		}
	case "textDocument/completion":
		from, ok := urlKeyAt(text, off)
		if !ok {
			return nil
		}
		dir, path := filepath.Dir(f.source), f.source
		return func() any {
			keys := packageURLKeys(dir, path, text)
			names := make([]string, 0, len(keys))
			for k := range keys {
				names = append(names, k)
			}
			sort.Strings(names)
			r := lspRange{positionOf(text, from), positionOf(text, off)}
			items := []any{}
			for _, k := range names {
				items = append(items, map[string]any{
					"label":  k,
					"kind":   12, // Value
					"detail": "kept by " + strings.Join(keys[k], ", "),
					"textEdit": map[string]any{
						"range":   r,
						"newText": k,
					},
				})
			}
			return map[string]any{"isIncomplete": false, "items": items}
		}
	}
	return nil
}
