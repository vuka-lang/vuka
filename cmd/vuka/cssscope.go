package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Scoped CSS: a component's stylesheet (web.Style, web.CSS) is rewritten so
// its selectors match only the elements the component's own markup renders,
// each of which carries data-vk-s="<scope>" (transpile.Options.Scopes):
//
//	output { … }               →  output[data-vk-s="1a2b3c"] { … }
//	.card .title::before { … } →  .card .title[data-vk-s="1a2b3c"]::before { … }
//	:global(.dark) .card { … } →  .dark .card[data-vk-s="1a2b3c"] { … }
//	.list :deep(li) { … }      →  .list[data-vk-s="1a2b3c"] li { … }
//	:scope { … }               →  [data-vk-s="1a2b3c"]:not([data-vk-s="1a2b3c"] *) { … }
//	:root { --gap: 1rem }      →  unchanged
//
// The scope's attribute goes on the last compound of each selector that
// isn't :global or :root and comes before any :deep; a selector with none
// stays unscoped. Nested rules (CSS nesting) are scoped the same way, but a
// compound with & is its parent's, already scoped. Rules inside @media,
// @supports, @container, @layer and @starting-style are scoped; @font-face,
// @property, @page and Tailwind's @theme, @utility and @variant pass through.
// A @keyframes is the component's own: its name gets the scope (bump →
// bump-1a2b3c), and so do the animation and animation-name declarations of
// the component's stylesheets naming it. Keyframes shared across components
// go in a global stylesheet.

// cssScoper rewrites one stylesheet for one scope.
type cssScoper struct {
	src    string
	dir    string // the file's directory, which relative @import paths start from
	label  string // the file, as messages name it
	base   int    // the line src starts at in the file (inline CSS), less one
	attr   string // [data-vk-s="<id>"]
	frames map[string]string
	out    strings.Builder
	sels   []cssName // the names its scoped selectors require, for checks
	err    *cssError
}

type cssError struct {
	label string
	line  int
	msg   string
}

func (e *cssError) Error() string { return fmt.Sprintf("%s:%d: %s", e.label, e.line, e.msg) }

// cssName is a class (.x), id (#x) or element (x) a selector requires.
type cssName struct {
	line int
	kind byte // '.', '#', or 0 for an element
	name string
}

func (n cssName) String() string {
	if n.kind == 0 {
		return n.name
	}
	return string(n.kind) + n.name
}

// scopeCSS rewrites src, a stylesheet of the component whose scope id is
// id, whose keyframes frames renames.
func scopeCSS(src, dir, label string, base int, id string, frames map[string]string) (string, []cssName, error) {
	c := &cssScoper{src: src, dir: dir, label: label, base: base, attr: `[data-vk-s="` + id + `"]`, frames: frames}
	i := c.rules(0, false)
	if c.err == nil && i < len(src) {
		c.fail(i, "unexpected }")
	}
	if c.err != nil {
		return "", nil, c.err
	}
	return c.out.String(), c.sels, nil
}

var keyframesDecl = regexp.MustCompile(`@(?:-webkit-)?keyframes\s+(-?[A-Za-z_][\w-]*)`)

// keyframesOf adds the names of the keyframes src declares to frames,
// scoped to id.
func keyframesOf(src, id string, frames map[string]string) {
	for _, m := range keyframesDecl.FindAllStringSubmatch(stripComments(src), -1) {
		frames[m[1]] = m[1] + "-" + id
	}
}

func stripComments(s string) string {
	for {
		i := strings.Index(s, "/*")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i+2:], "*/")
		if j < 0 {
			return s[:i]
		}
		s = s[:i] + strings.Repeat(" ", j+4) + s[i+j+4:]
	}
}

func (c *cssScoper) line(i int) int { return c.base + strings.Count(c.src[:i], "\n") + 1 }

func (c *cssScoper) fail(i int, format string, args ...any) {
	if c.err == nil {
		c.err = &cssError{c.label, c.line(i), fmt.Sprintf(format, args...)}
	}
}

// space copies whitespace and comments from i.
func (c *cssScoper) space(i int) int {
	for i < len(c.src) && c.err == nil {
		switch {
		case strings.IndexByte(" \t\r\n\f", c.src[i]) >= 0:
			c.out.WriteByte(c.src[i])
			i++
		case strings.HasPrefix(c.src[i:], "/*"):
			end := strings.Index(c.src[i+2:], "*/")
			if end < 0 {
				c.fail(i, "unclosed comment")
				return len(c.src)
			}
			c.out.WriteString(c.src[i : i+end+4])
			i += end + 4
		default:
			return i
		}
	}
	return i
}

// scan finds the first of stops at i or after, outside strings, comments,
// parentheses and brackets.
func (c *cssScoper) scan(i int, stops string) int {
	depth := 0
	for i < len(c.src) && c.err == nil {
		ch := c.src[i]
		switch {
		case ch == '/' && strings.HasPrefix(c.src[i:], "/*"):
			end := strings.Index(c.src[i+2:], "*/")
			if end < 0 {
				c.fail(i, "unclosed comment")
				return len(c.src)
			}
			i += end + 4
			continue
		case ch == '"' || ch == '\'':
			i = c.str(i)
			continue
		case ch == '\\':
			i += 2
			continue
		case ch == '(' || ch == '[':
			depth++
		case ch == ')' || ch == ']':
			if depth > 0 {
				depth--
			}
		case depth == 0 && strings.IndexByte(stops, ch) >= 0:
			return i
		}
		i++
	}
	return min(i, len(c.src))
}

// str returns where the string at i ends.
func (c *cssScoper) str(i int) int {
	q := c.src[i]
	for j := i + 1; j < len(c.src); j++ {
		switch c.src[j] {
		case '\\':
			j++
		case '\n':
			c.fail(i, "unclosed string")
			return j
		case q:
			return j + 1
		}
	}
	c.fail(i, "unclosed string")
	return len(c.src)
}

// block returns where the block opened by the { at open ends, just after its }.
func (c *cssScoper) block(open int) int {
	i := open + 1
	for c.err == nil {
		i = c.scan(i, "{}")
		switch {
		case i >= len(c.src):
			c.fail(open, "unclosed {")
			return len(c.src)
		case c.src[i] == '{':
			i = c.block(i)
		default:
			return i + 1
		}
	}
	return i
}

// rules copies the rules from i up to a } (returned) or the end; nested,
// they are inside a style rule, where declarations go too.
func (c *cssScoper) rules(i int, nested bool) int {
	for c.err == nil {
		if i = c.space(i); i >= len(c.src) || c.src[i] == '}' {
			return i
		}
		if c.src[i] == '@' {
			i = c.atRule(i, nested)
			continue
		}
		j := c.scan(i, ";{}")
		switch {
		case j < len(c.src) && c.src[j] == '{':
			if strings.TrimSpace(c.src[i:j]) == "" {
				c.fail(i, "a rule needs a selector before {")
				return j
			}
			c.out.WriteString(c.selectors(c.src[i:j], i))
			c.out.WriteByte('{')
			i = c.close(c.rules(j+1, true), j)
		case nested:
			if j < len(c.src) && c.src[j] == ';' {
				j++
			}
			c.out.WriteString(c.declaration(c.src[i:j]))
			i = j
		default:
			c.fail(i, "expected { after %s", strings.TrimSpace(c.src[i:j]))
			return j
		}
	}
	return i
}

// close copies the } at i that closes the block opened at open.
func (c *cssScoper) close(i, open int) int {
	if c.err != nil {
		return i
	}
	if i >= len(c.src) {
		c.fail(open, "unclosed {")
		return i
	}
	c.out.WriteByte('}')
	return i + 1
}

var atName = regexp.MustCompile(`^@([\w-]+)`)

// atRule copies the at-rule at i, scoping the rules of a conditional
// group and the name of a @keyframes.
func (c *cssScoper) atRule(i int, nested bool) int {
	name := strings.ToLower(atName.FindStringSubmatch(c.src[i:] + " ")[1])
	j := c.scan(i, ";{}")
	prelude := c.src[i:j]
	if j >= len(c.src) || c.src[j] != '{' {
		semi := j < len(c.src) && c.src[j] == ';'
		if semi {
			j++
		}
		if name == "import" || name == "reference" {
			prelude = importPath.ReplaceAllStringFunc(prelude, func(q string) string {
				return strconv.Quote(filepath.ToSlash(filepath.Join(c.dir, q[1:len(q)-1])))
			})
		}
		c.out.WriteString(prelude)
		if semi {
			c.out.WriteByte(';')
		}
		return j
	}
	switch name {
	case "media", "supports", "container", "layer", "document", "starting-style":
		c.out.WriteString(prelude + "{")
		return c.close(c.rules(j+1, nested), j)
	case "keyframes", "-webkit-keyframes":
		if m := keyframesDecl.FindStringSubmatchIndex(prelude); m != nil {
			if to, ok := c.frames[prelude[m[2]:m[3]]]; ok {
				prelude = prelude[:m[2]] + to + prelude[m[3]:]
			}
		}
	}
	end := c.block(j)
	c.out.WriteString(prelude + c.src[j:end])
	return end
}

// importPath is a relative path an @import or @reference names, which
// moves with the stylesheet into the generated file.
var importPath = regexp.MustCompile(`"\.\.?/[^"]*"|'\.\.?/[^']*'`)

var animationName = regexp.MustCompile(`-?[A-Za-z_][\w-]*`)

// declaration copies a declaration, the keyframes an animation names scoped.
func (c *cssScoper) declaration(d string) string {
	prop, value, ok := strings.Cut(d, ":")
	p := strings.ToLower(strings.TrimSpace(prop))
	if !ok || len(c.frames) == 0 || !(p == "animation" || p == "animation-name" || p == "-webkit-animation" || p == "-webkit-animation-name") {
		return d
	}
	return prop + ":" + animationName.ReplaceAllStringFunc(value, func(w string) string {
		if to, ok := c.frames[w]; ok {
			return to
		}
		return w
	})
}

// compound is a compound selector and the combinator before it.
type compound struct {
	comb, text                      string
	global, deep, root, scoped, amp bool
}

// selectors scopes a selector list starting at off.
func (c *cssScoper) selectors(list string, off int) string {
	lead := list[:len(list)-len(strings.TrimLeft(list, " \t\r\n"))]
	trail := list[len(strings.TrimRight(list, " \t\r\n")):]
	var out []string
	for _, sel := range splitTop(strings.TrimSpace(list), ',') {
		out = append(out, c.selector(strings.TrimSpace(sel), c.line(off)))
	}
	return lead + strings.Join(out, ", ") + trail
}

func (c *cssScoper) selector(sel string, line int) string {
	cs := compounds(sel)
	deep := len(cs)
	for i := range cs {
		cs[i].text = c.pseudo(&cs[i], ":global(", &cs[i].global)
		cs[i].text = c.pseudo(&cs[i], ":deep(", &cs[i].deep)
		if cs[i].deep && deep == len(cs) {
			deep = i
		}
		lower := strings.ToLower(cs[i].text)
		cs[i].root = hasPseudo(lower, ":root")
		if hasPseudo(lower, ":scope") {
			cs[i].scoped = true
			cs[i].text = replacePseudo(cs[i].text, ":scope", c.attr+":not("+c.attr+" *)")
		}
		cs[i].amp = strings.Contains(stripParens(cs[i].text), "&")
	}
	target := -1
	for i := 0; i < deep; i++ {
		if !cs[i].global && !cs[i].root {
			target = i
		}
		if !cs[i].global && !cs[i].root && !cs[i].scoped && !cs[i].amp {
			c.sels = append(c.sels, namesOf(cs[i].text, line)...)
		}
	}
	prefix := ""
	switch {
	case target >= 0 && !cs[target].scoped && !cs[target].amp:
		cs[target].text = withAttr(cs[target].text, c.attr)
	case target < 0 && deep == 0 && len(cs) > 0:
		prefix = c.attr + " "
	}
	var b strings.Builder
	b.WriteString(prefix)
	for i, cp := range cs {
		if i > 0 || cp.comb != " " {
			b.WriteString(cp.comb)
		}
		b.WriteString(cp.text)
	}
	return strings.TrimSpace(b.String())
}

// pseudo replaces :global(x) or :deep(x) in a compound by x, setting flag.
func (c *cssScoper) pseudo(cp *compound, name string, flag *bool) string {
	t := cp.text
	i := strings.Index(strings.ToLower(t), name)
	if i < 0 {
		return t
	}
	depth, j := 1, i+len(name)
	for ; j < len(t) && depth > 0; j++ {
		switch t[j] {
		case '(':
			depth++
		case ')':
			depth--
		}
	}
	*flag = true
	inner := strings.TrimSpace(t[i+len(name) : j-1])
	return t[:i] + inner + t[j:]
}

func hasPseudo(s, name string) bool {
	for i := 0; ; {
		k := strings.Index(s[i:], name)
		if k < 0 {
			return false
		}
		end := i + k + len(name)
		if end >= len(s) || !isCSSNameByte(s[end]) {
			return true
		}
		i = end
	}
}

func replacePseudo(s, name, with string) string {
	i := strings.Index(strings.ToLower(s), name)
	return s[:i] + with + s[i+len(name):]
}

func isCSSNameByte(b byte) bool {
	return b == '-' || b == '_' || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z' || '0' <= b && b <= '9' || b >= 0x80
}

// withAttr adds attr to a compound, before its pseudo-element if any.
func withAttr(t, attr string) string {
	lower := strings.ToLower(t)
	depth := 0
	for i := 0; i < len(t); i++ {
		switch t[i] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case '\\':
			i++
		case ':':
			if depth > 0 {
				continue
			}
			if strings.HasPrefix(t[i:], "::") {
				return t[:i] + attr + t[i:]
			}
			for _, legacy := range []string{":before", ":after", ":first-line", ":first-letter"} {
				if strings.HasPrefix(lower[i:], legacy) && (i+len(legacy) == len(t) || !isCSSNameByte(t[i+len(legacy)])) {
					return t[:i] + attr + t[i:]
				}
			}
		}
	}
	return t + attr
}

// compounds splits a complex selector at its combinators.
func compounds(sel string) []compound {
	var out []compound
	comb, cur := " ", strings.Builder{}
	depth := 0
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, compound{comb: comb, text: cur.String()})
			cur.Reset()
			comb = " "
		}
	}
	for i := 0; i < len(sel); i++ {
		ch := sel[i]
		switch {
		case ch == '\\' && i+1 < len(sel):
			cur.WriteByte(ch)
			i++
			cur.WriteByte(sel[i])
			continue
		case ch == '"' || ch == '\'':
			j := strings.IndexByte(sel[i+1:], ch)
			if j < 0 {
				j = len(sel) - i - 1
			}
			cur.WriteString(sel[i : i+j+2])
			i += j + 1
			continue
		case ch == '(' || ch == '[':
			depth++
		case ch == ')' || ch == ']':
			depth--
		case depth == 0 && (ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'):
			flush()
			continue
		case depth == 0 && (ch == '>' || ch == '+' || ch == '~'):
			flush()
			comb = " " + string(ch) + " "
			continue
		}
		cur.WriteByte(ch)
	}
	flush()
	return out
}

// splitTop splits s at sep outside parentheses, brackets and strings.
func splitTop(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch ch := s[i]; {
		case ch == '\\':
			i++
		case ch == '"' || ch == '\'':
			if j := strings.IndexByte(s[i+1:], ch); j >= 0 {
				i += j + 1
			}
		case ch == '(' || ch == '[':
			depth++
		case ch == ')' || ch == ']':
			depth--
		case ch == sep && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// stripParens drops what is inside parentheses and brackets.
func stripParens(t string) string {
	var b strings.Builder
	depth := 0
	for i := 0; i < len(t); i++ {
		switch t[i] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		default:
			if depth == 0 {
				b.WriteByte(t[i])
			}
		}
	}
	return b.String()
}

var selName = regexp.MustCompile(`([.#])((?:[\w-]|\\.|[^\x00-\x7f])+)|^([A-Za-z][\w-]*)`)

// namesOf lists the classes, ids and element a compound requires, outside
// its pseudo-classes' arguments and attribute selectors.
func namesOf(t string, line int) []cssName {
	var out []cssName
	for _, m := range selName.FindAllStringSubmatch(stripParens(t), -1) {
		switch {
		case m[1] != "":
			out = append(out, cssName{line, m[1][0], strings.ReplaceAll(m[2], `\`, "")})
		case m[3] != "":
			out = append(out, cssName{line, 0, strings.ToLower(m[3])})
		}
	}
	return out
}
