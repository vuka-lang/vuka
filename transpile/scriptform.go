package transpile

import (
	"go/scanner"
	"go/token"
	"regexp"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
)

// A .vuka.html file declares its struct one of two ways. The script form,
// canonical since 2.3, holds the Go in a <script lang="go"> element, the
// file's first; the markup is what follows it:
//
//	<script lang="go">
//	package counter
//
//	type Index struct {
//		ui.Live
//		count int
//	}
//
//	func (p *Index) Add() { p.count++ }
//	</script>
//
//	<main>
//	  <h1>{count}</h1>
//	  <button onClick={Add}>Add one</button>
//	</main>
//
// The header form, the 2.0 to 2.2 one, writes the same Go bare above the
// markup, which starts at the first line starting with < (htmlshape.go). It
// still compiles in 2.x; vukavet reports it (W0107) and `vuka fix
// shape-script` rewrites it. The compiler reads a script-form file through
// HeaderView: the same text with the script's tags written as spaces, which
// is a header-form file at the same offsets, so every position — the
// compiler's, gopls', a source map's — is the file's own.

// TemplateParts is where a template file's parts are, as byte offsets in
// the file.
type TemplateParts struct {
	// Script is the <script lang="go"> element; zero in the header form.
	Script ScriptBlock

	// GoStart and GoEnd bound the file's Go: the script's content, or in
	// the header form the file from its start up to its markup.
	GoStart, GoEnd int

	// Markup is where the markup starts, -1 when nothing follows the Go.
	Markup int
}

// ScriptBlock is a <script lang="go"> element.
type ScriptBlock struct {
	Open, OpenEnd   int // the start tag: src[Open:OpenEnd] is <script lang="go">
	Close, CloseEnd int // the end tag: src[Close:CloseEnd] is </script>
}

// IsScript reports whether the file is in the script form.
func (p TemplateParts) IsScript() bool { return p.Script.OpenEnd > 0 }

// InGo reports whether the offset off is in the file's Go: the script's
// content, or the header form's Go header.
func (p TemplateParts) InGo(off int) bool { return off >= p.GoStart && off < p.GoEnd }

var (
	scriptOpen = regexp.MustCompile(`(?i)^<script\b([^>]*)>`)
	goLang     = regexp.MustCompile(`(?i)(?:^|\s)lang\s*=\s*(?:"go"|'go'|go(?:\s|$))`)
	anyGoTag   = regexp.MustCompile(`(?i)<script\b[^>]*\blang\s*=\s*(?:"go"|'go'|go[\s>])`)
)

// SplitTemplate finds the parts of the template file src: its
// <script lang="go"> element when it has one, its Go and its markup. It is
// what the compiler, the formatter and the language server read the file's
// boundaries with. The error is a script form broken: a <script lang="go">
// with no </script> (E0156), or one that isn't the file's first element, or
// a second one (E0157).
func SplitTemplate(src []byte) (TemplateParts, *Error) {
	p := TemplateParts{Markup: -1}
	sb, ok, err := scriptBlock(src)
	if err != nil {
		return p, err
	}
	if ok {
		p.Script = sb
		p.GoStart, p.GoEnd = sb.OpenEnd, sb.Close
		rest := sb.CloseEnd
		for rest < len(src) && isSpaceByte(src[rest]) {
			rest++
		}
		if rest < len(src) {
			p.Markup = rest
		}
		return p, nil
	}
	// The header form, or no Go at all.
	head := parseTemplateHead(src)
	lines := strings.SplitAfter(string(src), "\n")
	at := 0
	for _, l := range lines[:head.Top] {
		at += len(l)
	}
	if !isShape(lines, head.Top) {
		p.GoEnd = at
		if at < len(src) {
			p.Markup = at
		}
		return p, nil
	}
	_, _, markup := goHeader(src, at)
	p.GoEnd, p.Markup = len(src), markup
	if markup >= 0 {
		for markup > 0 && src[markup-1] != '\n' {
			markup--
		}
		p.GoEnd, p.Markup = markup, markup
	}
	return p, nil
}

// scriptBlock is the template file's <script lang="go">, and whether it
// has one; its error is SplitTemplate's.
func scriptBlock(src []byte) (ScriptBlock, bool, *Error) {
	var sb ScriptBlock
	start := leadingTrivia(src)
	m := scriptOpen.FindSubmatchIndex(src[start:])
	if m == nil || !goLang.Match(src[start+m[2]:start+m[3]]) {
		if loc := anyGoTag.FindIndex(src); loc != nil {
			return sb, false, posError(src, loc[0], loc[1]-loc[0], "E0157",
				"a <script lang=\"go\"> is the file's first element: only blank lines and <!-- comments --> come before it",
				"move it to the top of the file, its markup below it")
		}
		return sb, false, nil
	}
	sb.Open, sb.OpenEnd = start, start+m[1]
	close := scriptClose(src, sb.OpenEnd)
	if close < 0 {
		return sb, false, posError(src, sb.Open, sb.OpenEnd-sb.Open, "E0156",
			"this <script lang=\"go\"> has no </script>: the file's Go runs to its end",
			"end the Go with </script>, the markup below it")
	}
	sb.Close = close
	sb.CloseEnd = close + strings.IndexByte(string(src[close:]), '>') + 1
	if loc := anyGoTag.FindIndex(src[sb.CloseEnd:]); loc != nil {
		at := sb.CloseEnd + loc[0]
		return sb, false, posError(src, at, loc[1]-loc[0], "E0157",
			"the file has a <script lang=\"go\"> already: a .vuka.html file holds one, its first element",
			"join their Go in the first one")
	}
	return sb, true, nil
}

// HeaderView is the template file src as the header form reads it: a
// script-form file with its <script lang="go"> tags, and the comments before
// them, written as spaces, the same length and its lines kept; any other
// file as it is. A script form broken (SplitTemplate's error) is the file as
// it is too.
func HeaderView(src []byte) []byte {
	out, _, _ := headerView(src)
	return out
}

// headerView is HeaderView, whether src is in the script form, and the
// error of a script form broken.
func headerView(src []byte) ([]byte, bool, *Error) {
	sb, ok, err := scriptBlock(src)
	if !ok {
		return src, false, err
	}
	out := []byte(string(src))
	blankBytes(out, 0, sb.OpenEnd)
	blankBytes(out, sb.Close, sb.CloseEnd)
	return out, true, nil
}

// blankBytes writes b[from:to] as spaces, its line breaks kept.
func blankBytes(b []byte, from, to int) {
	for i := max(from, 0); i < to && i < len(b); i++ {
		if b[i] != '\n' && b[i] != '\r' {
			b[i] = ' '
		}
	}
}

func isSpaceByte(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' }

// leadingTrivia is the offset past the blank space and <!-- comments --> a
// file starts with.
func leadingTrivia(src []byte) int {
	i := 0
	for {
		for i < len(src) && isSpaceByte(src[i]) {
			i++
		}
		if !strings.HasPrefix(string(src[i:min(i+4, len(src))]), "<!--") {
			return i
		}
		end := strings.Index(string(src[i+4:]), "-->")
		if end < 0 {
			return i
		}
		i += 4 + end + 3
	}
}

// scriptClose is the offset of the </script> ending the Go from the offset
// from, -1 when none does. The Go is read as tokens, so a </script> in a
// string or a comment doesn't end it.
func scriptClose(src []byte, from int) int {
	text := src[from:]
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(text))
	var sc scanner.Scanner
	sc.Init(file, text, func(token.Position, string) {}, scanner.ScanComments)
	type tok struct {
		t   token.Token
		off int
		lit string
	}
	var last [3]tok // the three tokens before the current one
	n := 0
	for {
		p, t, lit := sc.Scan()
		if t == token.EOF {
			return -1
		}
		if t == token.SEMICOLON && lit == "\n" {
			continue
		}
		o := file.Offset(p)
		// < / script >, the first three touching.
		if t == token.GTR && n >= 3 && last[0].t == token.LSS && last[1].t == token.QUO && last[2].t == token.IDENT &&
			strings.EqualFold(last[2].lit, "script") && last[1].off == last[0].off+1 && last[2].off == last[1].off+1 &&
			strings.TrimSpace(string(text[last[2].off+len(last[2].lit):o])) == "" {
			return from + last[0].off
		}
		last[0], last[1], last[2] = last[1], last[2], tok{t, o, lit}
		n++
	}
}

// posError is an error at the offset off of src, n bytes long.
func posError(src []byte, off, n int, code diag.Code, msg, help string) *Error {
	line := 1 + strings.Count(string(src[:off]), "\n")
	col := off - (strings.LastIndexByte(string(src[:off]), '\n') + 1) + 1
	return &Error{Code: code, Pos: token.Position{Line: line, Column: col, Offset: off}, Len: n, Msg: msg, Help: help}
}

// ToScriptForm is the header-form template file src rewritten in the script
// form: its Go — package clause, imports, struct, methods — in a
// <script lang="go"> above its markup, the rest as it was. ok is false, and
// src is returned, when there is nothing to rewrite: the file is in the
// script form already, or has no Go header or no markup (E0152, E0148).
// Rewriting a rewritten file changes nothing.
func ToScriptForm(src []byte) (out []byte, ok bool) {
	p, err := SplitTemplate(src)
	if err != nil || p.IsScript() || p.Markup < 0 {
		return src, false
	}
	if sh, err := ParseShape(src); sh == nil || err != nil && err.Code == "E0148" {
		return src, false
	}
	nl := "\n"
	if strings.Contains(string(src[:p.Markup]), "\r\n") {
		nl = "\r\n"
	}
	goText := strings.TrimRight(strings.TrimLeft(string(src[:p.Markup]), "\r\n"), " \t\r\n")
	var b strings.Builder
	b.WriteString(`<script lang="go">` + nl)
	b.WriteString(goText)
	b.WriteString(nl + "</script>" + nl + nl)
	b.Write(src[p.Markup:])
	return []byte(b.String()), true
}
