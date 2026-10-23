package format

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// Template formats a .vuka.html file with the default options; see
// TemplateWith.
func Template(src []byte, pkg string) ([]byte, error) { return TemplateWith(src, pkg, Options{}) }

// TemplateWith formats a .vuka.html file, in the form it is in — the
// <script lang="go"> one or the Go-header one:
//
//   - its package clause first (pkg's, added when the file has none and pkg
//     isn't "");
//   - its Go as vuka fmt formats a .vuka file, unindented in the script;
//   - its markup indented two spaces a level — elements, and the blocks of
//     its Go statements — those statements and the Go of its holes as gofmt
//     writes them, an opening tag's attributes one a line when it doesn't
//     fit in o's width, void elements and childless components closing
//     themselves (<input … />, <ui.Badge />), with no runs of blank lines;
//   - a top-level <style>'s CSS formatted (CSS), a blank line before it
//     and before a component's <script>;
//   - with o.Tailwind, a static class list's utilities in Tailwind's order.
//
// It changes only whitespace that doesn't render: text stays on its lines,
// and what <pre>, <textarea>, <script> and an inline <style> hold stays as
// written. Formatting a formatted file changes nothing.
func TemplateWith(src []byte, pkg string, o Options) ([]byte, error) {
	if !utf8.Valid(src) {
		return nil, errors.New("not valid UTF-8")
	}
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	if pkg != "" {
		text = string(TemplatePackage([]byte(text), pkg))
	}
	b := []byte(text)
	parts, perr := transpile.SplitTemplate(b)
	if perr != nil {
		return nil, fmt.Errorf("%d:%d: %s", perr.Pos.Line, perr.Pos.Column, perr.Msg)
	}
	var out strings.Builder
	markupAt := parts.Markup
	switch {
	case parts.IsScript():
		lead := strings.TrimSpace(text[:parts.Script.Open])
		if lead != "" {
			out.WriteString(lead + "\n")
		}
		goText := text[parts.GoStart:parts.GoEnd]
		f, err := formatGo(goText, lineOf(text, parts.GoStart)-1)
		if err != nil {
			return nil, err
		}
		out.WriteString("<script lang=\"go\">\n" + f + "</script>\n")
		if markupAt >= 0 {
			out.WriteString("\n")
		}
	case markupAt != 0 && strings.TrimSpace(text[:max(markupAt, 0)]) != "" || markupAt < 0:
		end := markupAt
		if end < 0 {
			end = len(text)
		}
		f, err := formatGo(text[:end], 0)
		if err != nil {
			return nil, err
		}
		out.WriteString(f)
		if sh, _ := transpile.ParseShape(b); sh == nil && markupAt >= 0 {
			out.WriteString("\n") // a head alone: a blank line before the markup
		}
	}
	if markupAt >= 0 {
		m, err := formatTemplateMarkup([]byte(text[markupAt:]), markupAt, o)
		if err != nil {
			line := lineOf(text, markupAt)
			if me, ok := err.(*markupError); ok {
				line = lineOf(text, me.off)
			}
			return nil, fmt.Errorf("%d:1: %v", line, err)
		}
		out.WriteString(m)
	}
	res := strings.TrimRight(out.String(), "\n") + "\n"
	return []byte(res), nil
}

// formatTemplateMarkup lays out a template's markup, checking that it
// renders what it did.
func formatTemplateMarkup(src []byte, offset int, o Options) (string, error) {
	m, err := formatMarkup(src, offset, o, func(css string) (string, error) { return CSS(css) })
	if err != nil {
		return "", err
	}
	before, err1 := templateSig(src)
	after, err2 := templateSig([]byte(m))
	if err1 != nil || err2 != nil || trimSig(before) != trimSig(after) {
		return "", errors.New("internal error: formatting would change what this markup renders; please report it")
	}
	return m, nil
}

// trimSig drops a signature's leading and trailing line breaks.
func trimSig(s string) string {
	for strings.HasPrefix(s, "⏎\x00") {
		s = strings.TrimPrefix(s, "⏎\x00")
	}
	for strings.HasSuffix(s, "⏎\x00") {
		s = strings.TrimSuffix(s, "⏎\x00")
	}
	return s
}

// formatGo formats a template's Go — a script's content or a Go header —
// as a .vuka file; its errors' lines are the file's, the Go starting below
// the line base.
func formatGo(text string, base int) (string, error) {
	f, err := Source([]byte(strings.TrimLeft(text, "\n")))
	if err != nil {
		lead := strings.Count(text, "\n") - strings.Count(strings.TrimLeft(text, "\n"), "\n")
		var line, col int
		var msg string
		if n, _ := fmt.Sscanf(err.Error(), "%d:%d:", &line, &col); n == 2 {
			msg = err.Error()[len(fmt.Sprintf("%d:%d:", line, col)):]
			return "", fmt.Errorf("%d:%d:%s", line+base+lead, col, msg)
		}
		return "", err
	}
	return string(f), nil
}

func lineOf(text string, off int) int { return 1 + strings.Count(text[:min(off, len(text))], "\n") }

var packageLine = regexp.MustCompile(`^package\s+\w+`)

// TemplatePackage is a template file starting with the package clause of
// pkg when it has none: above its leading comment, unless a blank line ends
// that comment (a file comment rather than a template's doc comment).
func TemplatePackage(src []byte, pkg string) []byte {
	if transpile.ParseTemplateHead(src).Package != "" {
		return src
	}
	if p, err := transpile.SplitTemplate(src); err == nil && p.IsScript() {
		// In the script, above its Go.
		at := p.Script.OpenEnd
		rest := strings.TrimLeft(string(src[at:]), " \t\r\n")
		return []byte(string(src[:at]) + "\npackage " + pkg + "\n\n" + rest)
	}
	text, at := string(src), 0
	for _, l := range strings.SplitAfter(text, "\n") {
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "//") {
			if t != "" || at == 0 {
				at = 0
			}
			break
		}
		at += len(l)
	}
	sep, rest := "", text[at:]
	for strings.HasPrefix(strings.TrimLeft(rest, " \t"), "\n") {
		rest, sep = rest[strings.IndexByte(rest, '\n')+1:], "\n"
	}
	if at == 0 {
		sep = ""
	}
	return []byte(text[:at] + sep + "package " + pkg + "\n\n" + rest)
}

// DirPackage is the package of the Go and .vuka files in dir, not an
// external test package's; else the directory's name.
func DirPackage(dir string) string {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") && !strings.HasSuffix(n, ".vuka") {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		if m := packageOf.FindSubmatch(src); m != nil && !bytes.HasSuffix(m[1], []byte("_test")) {
			return string(m[1])
		}
	}
	abs, _ := filepath.Abs(dir)
	name := strings.Map(func(r rune) rune {
		if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}
		return -1
	}, strings.ToLower(filepath.Base(abs)))
	if name == "" || unicode.IsDigit(rune(name[0])) {
		return "main"
	}
	return name
}

var packageOf = regexp.MustCompile(`(?m)^package\s+(\w+)`)

// holesFormatted is a line with each hole's Go as gofmt writes an
// expression: {a+b} is {a + b}. A hole spanning lines, or not an
// expression, stays.
func holesFormatted(l string) string {
	var b bytes.Buffer
	for i := 0; i < len(l); i++ {
		if l[i] != '{' || i+1 < len(l) && (l[i+1] == '{' || l[i+1] == '/') {
			if l[i] == '{' && i+1 < len(l) && l[i+1] == '{' {
				b.WriteString("{{")
				i++
				continue
			}
			b.WriteByte(l[i])
			continue
		}
		end := closeBrace(l, i+1)
		if end < 0 {
			b.WriteString(l[i:])
			return b.String()
		}
		expr := strings.TrimSpace(l[i+1 : end])
		spread := strings.HasSuffix(expr, "...")
		expr = strings.TrimSuffix(expr, "...")
		if x, err := parser.ParseExpr(expr); err == nil {
			var f bytes.Buffer
			if format.Node(&f, token.NewFileSet(), x) == nil && !bytes.Contains(f.Bytes(), []byte("\n")) {
				expr = f.String()
			}
		}
		if spread {
			expr += "..."
		}
		b.WriteString("{" + expr + "}")
		i = end
	}
	return b.String()
}

// closeBrace is the } closing the Go at l[i:], past brackets and literals.
func closeBrace(l string, i int) int {
	depth := 0
	for ; i < len(l); i++ {
		switch c := l[i]; c {
		case '"', '\'', '`':
			j := i + 1
			for j < len(l) && l[j] != c {
				if l[j] == '\\' && c != '`' {
					j++
				}
				j++
			}
			i = j
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				if c == '}' {
					return i
				}
				return -1
			}
			depth--
		}
	}
	return -1
}
