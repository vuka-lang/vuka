package check

import (
	"go/scanner"
	"go/token"
	"regexp"
	"strings"
)

// A .vuka.html file in the 2.0 shape is Go above markup: its struct, hooks
// and methods, then the markup it renders from the first line starting with
// < outside the Go (the compiler's transpile.ParseShape, which checks its
// rules). Its templates start at that markup, and the headerless one is the
// struct's.

var (
	shapeFirst  = regexp.MustCompile(`^(?:type|var|const)\b|^@[A-Za-z_]`)
	shapeStruct = regexp.MustCompile(`(?m)^type\s+(\w+)(?:\[[^\]]*\])?\s+struct\b`)
)

// shapeOf is the struct a file in the 2.0 shape renders and the offset its
// markup starts at; ok is false for a file of templates, or one whose
// markup can't be found.
func shapeOf(src string) (typ string, markup int, ok bool) {
	lines := strings.SplitAfter(src, "\n")
	off, top := 0, -1
	group := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case group:
			group = t != ")"
		case t == "import (":
			group = true
		case t == "" || strings.HasPrefix(t, "//") || importLine.MatchString(t) || packageLine.MatchString(t):
		default:
			top = i
		}
		if top >= 0 {
			break
		}
		off += len(l)
	}
	if top < 0 || !shapeFirst.MatchString(lines[top]) && !structBeforeMarkup(lines[top:]) {
		return "", 0, false
	}
	text := []byte(src[off:])
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(text))
	var sc scanner.Scanner
	sc.Init(file, text, func(token.Position, string) {}, scanner.ScanComments)
	depth := 0
	for {
		p, t, _ := sc.Scan()
		if t == token.EOF {
			return "", 0, false
		}
		o := file.Offset(p)
		if depth == 0 && (t == token.LSS || t == token.SHL || t == token.LEQ) && strings.TrimSpace(string(text[strings.LastIndexByte(string(text[:o]), '\n')+1:o])) == "" {
			m := shapeStruct.FindStringSubmatch(string(text[:o]))
			if m == nil {
				return "", 0, false
			}
			return m[1], off + o, true
		}
		switch t {
		case token.LBRACE, token.LPAREN, token.LBRACK:
			depth++
		case token.RBRACE, token.RPAREN, token.RBRACK:
			depth--
		}
	}
}

func structBeforeMarkup(lines []string) bool {
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "<") {
			return false
		}
		if shapeStruct.MatchString(l) {
			return true
		}
	}
	return false
}
