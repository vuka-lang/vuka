package check

import (
	"go/scanner"
	"go/token"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// A .vuka.html file in the 2.0 shape is Go above markup: its struct, hooks
// and methods, then the markup it renders. In the script form, canonical
// since 2.3, the Go is a <script lang="go">, the file's first element, and
// the markup follows it; in the header form it is bare, and the markup
// starts at the first line starting with < outside the Go (the compiler's
// transpile.SplitTemplate and ParseShape, which check their rules). Its
// templates start at that markup, and the headerless one is the struct's.
// The checks read a script-form file as the header form (scriptView): its
// script's tags blanked, its offsets the file's.

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

var (
	scriptOpen = regexp.MustCompile(`(?i)^<script\b([^>]*)>`)
	goLang     = regexp.MustCompile(`(?i)(?:^|\s)lang\s*=\s*(?:"go"|'go'|go(?:\s|$))`)
)

// scriptView is src with its <script lang="go"> tags written as spaces, and
// whether it has them: a file in the script form read as the header form,
// at the same offsets. The script's end is found by Go tokens, as the
// compiler finds it; a script with no end is the file as it is.
func scriptView(src string) (string, bool) {
	start := len(src) - len(strings.TrimLeft(src, " \t\r\n"))
	for strings.HasPrefix(src[start:], "<!--") {
		e := strings.Index(src[start:], "-->")
		if e < 0 {
			return src, false
		}
		start += e + 3
		start += len(src[start:]) - len(strings.TrimLeft(src[start:], " \t\r\n"))
	}
	m := scriptOpen.FindStringSubmatchIndex(src[start:])
	if m == nil || !goLang.MatchString(src[start+m[2]:start+m[3]]) {
		return src, false
	}
	openEnd := start + m[1]
	text := []byte(src[openEnd:])
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(text))
	var sc scanner.Scanner
	sc.Init(file, text, func(token.Position, string) {}, scanner.ScanComments)
	type tok struct {
		t   token.Token
		off int
		lit string
	}
	var last [3]tok
	for {
		p, t, lit := sc.Scan()
		if t == token.EOF {
			return src, false
		}
		if t == token.SEMICOLON && lit == "\n" {
			continue
		}
		o := file.Offset(p)
		if t == token.GTR && last[0].t == token.LSS && last[1].t == token.QUO && last[2].t == token.IDENT &&
			strings.EqualFold(last[2].lit, "script") && last[1].off == last[0].off+1 && last[2].off == last[1].off+1 {
			closeAt, closeEnd := openEnd+last[0].off, openEnd+o+1
			b := []byte(src)
			for i := range b[:closeEnd] {
				if (i < openEnd || i >= closeAt) && b[i] != '\n' && b[i] != '\r' {
					b[i] = ' '
				}
			}
			return string(b), true
		}
		last[0], last[1], last[2] = last[1], last[2], tok{t, o, lit}
	}
}

// headerForm reports the files of the package in the Go-header form
// (W0107), with the fix that writes their Go in a <script lang="go">.
func headerForm(pass *analysis.Pass, html []*htmlSource) {
	for _, h := range html {
		if h.script || h.typ == "" || h.markup < 0 {
			continue
		}
		start := len(h.src) - len(strings.TrimLeft(h.src, " \t\r\n"))
		markup := strings.LastIndexByte(h.src[:h.markup], '\n') + 1
		end := markup
		for end > start && strings.ContainsRune(" \t\r\n", rune(h.src[end-1])) {
			end--
		}
		first := strings.IndexByte(h.src[start:], '\n')
		if first < 0 {
			first = len(h.src) - start
		}
		msg := "W0107: " + h.typ + "'s Go is a header above its markup: write it in a <script lang=\"go\">, the file's first element" +
			"\n\thelp: vuka fix shape-script rewrites every file so; the header form is removed in 3.0"
		pass.Report(analysis.Diagnostic{
			Pos: h.file.Pos(start), End: h.file.Pos(start + first), Category: "W0107", URL: docsURL + "W0107", Message: msg,
			SuggestedFixes: []analysis.SuggestedFix{{
				Message: "Write the Go in <script lang=\"go\">",
				TextEdits: []analysis.TextEdit{
					{Pos: h.file.Pos(start), End: h.file.Pos(start), NewText: []byte("<script lang=\"go\">\n")},
					{Pos: h.file.Pos(end), End: h.file.Pos(markup), NewText: []byte("\n</script>\n\n")},
				},
			}},
		})
	}
}
