package format

import (
	"bytes"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/vuka-lang/vuka/v2/transpile"
)

var (
	funcHeader = regexp.MustCompile(`^func\s`)
	rawBlock   = regexp.MustCompile(`^\s*<(script|style)\b`)
)

// Template formats a .vuka.html file: its package clause first (pkg's,
// added when the file has none and pkg isn't ""), the Go of its func
// headers and of holes on one line written as gofmt writes it, trailing
// space gone, no runs of blank lines, one blank line before each header (or
// its doc comment), none between a doc comment and its header or a header
// and its markup, and one newline at the end. Markup, the Go header above
// it (a struct on one line stays one), and the content of scripts and
// styles stay as written.
func Template(src []byte, pkg string) ([]byte, error) {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	if pkg != "" {
		text = string(TemplatePackage([]byte(text), pkg))
	}
	lines := strings.Split(text, "\n")
	var out []string
	raw := ""
	header, clause, body := false, false, false
	markup := false // past the Go header: its first line starting with <
	for _, l := range lines {
		l = strings.TrimRight(l, " \t")
		markup = markup || strings.HasPrefix(l, "<")
		if l == "" && (header || clause) && raw == "" {
			continue
		}
		if clause && l != "" {
			out = append(out, "")
		}
		header, clause = false, false
		switch {
		case raw != "":
			if strings.Contains(l, "</"+raw+">") {
				raw = ""
			}
		case rawBlock.MatchString(l):
			if m := rawBlock.FindStringSubmatch(l); !strings.Contains(l, "</"+m[1]+">") {
				raw = m[1]
			}
		case funcHeader.MatchString(l):
			if f, err := format.Source([]byte("package p\n" + l + " {}\n")); err == nil {
				// gofmt writes a long header's body on lines of its own
				l = strings.TrimPrefix(string(f), "package p\n\n")
				l = strings.TrimSuffix(strings.TrimSuffix(l, " {}\n"), " {\n}\n")
				markup = true // a template's header: its markup follows
			}
			for len(out) > 0 && out[len(out)-1] == "" {
				out = out[:len(out)-1]
			}
			doc := len(out)
			for doc > 0 && strings.HasPrefix(out[doc-1], "//") {
				doc--
			}
			if doc > 0 && out[doc-1] != "" {
				out = append(out[:doc], append([]string{""}, out[doc:]...)...)
			}
			header = true
		case packageLine.MatchString(l) && !body:
			clause = true
		case markup:
			l = holesFormatted(l)
		}
		if l == "" && len(out) > 0 && out[len(out)-1] == "" && raw == "" {
			continue
		}
		body = body || l != "" && !strings.HasPrefix(l, "//")
		out = append(out, l)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return []byte(strings.Join(out, "\n") + "\n"), nil
}

var packageLine = regexp.MustCompile(`^package\s+\w+`)

// TemplatePackage is a template file starting with the package clause of
// pkg when it has none: above its leading comment, unless a blank line ends
// that comment (a file comment rather than a template's doc comment).
func TemplatePackage(src []byte, pkg string) []byte {
	if transpile.ParseTemplateHead(src).Package != "" {
		return src
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
