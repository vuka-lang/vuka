package format

import (
	"bytes"
	"go/format"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

var (
	funcHeader = regexp.MustCompile(`^func\s`)
	rawBlock   = regexp.MustCompile(`^\s*<(script|style)\b`)
)

// Template formats a .vuka.html file: the Go of its func headers and of
// holes on one line written as gofmt writes it, trailing space gone, one
// blank line between templates and no runs of them. Markup, and the
// content of scripts and styles, stay as written.
func Template(src []byte) ([]byte, error) {
	lines := strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n")
	var out []string
	raw := ""
	for _, l := range lines {
		l = strings.TrimRight(l, " \t")
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
				l = strings.TrimSuffix(strings.TrimPrefix(string(f), "package p\n\n"), " {}\n")
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
		default:
			l = holesFormatted(l)
		}
		if l == "" && len(out) > 0 && out[len(out)-1] == "" && raw == "" {
			continue
		}
		out = append(out, l)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return []byte(strings.Join(out, "\n") + "\n"), nil
}

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
