package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/docs"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// diagJSON is set by -diag=json: diagnostics go to stderr as one JSON array
// instead of code frames.
var diagJSON bool

// cutDiagFlag takes -diag=json (or -diag=text) out of a go command's args.
func cutDiagFlag(args []string) []string {
	for i, a := range args {
		if a == "--" || !strings.HasPrefix(a, "-") {
			break
		}
		if v, ok := strings.CutPrefix(strings.TrimLeft(a, "-"), "diag="); ok {
			diagJSON = v == "json"
			return append(slices.Clip(args[:i]), args[i+1:]...)
		}
	}
	return args
}

// printErr prints err: its diagnostics as code frames (or JSON), the rest as
// it is.
func printErr(w *os.File, err error) {
	ds, rest := diag.From(err)
	if len(ds) > 0 {
		writeDiags(w, ds, diag.ColorFor(w))
	}
	for _, e := range rest {
		fmt.Fprintln(w, e)
	}
}

// writeDiags renders diagnostics for the terminal, files relative to the
// current directory, each without help given its code's.
func writeDiags(w io.Writer, ds []diag.Diagnostic, color bool) {
	wd, _ := os.Getwd()
	if r, err := filepath.EvalSymlinks(wd); err == nil {
		wd = r
	}
	ds = docs.Fill(relDiags(slices.Clone(ds), wd))
	if diagJSON {
		w.Write(append(diag.JSON(ds), '\n'))
		return
	}
	diag.Render(w, ds, diag.Options{Color: color, Root: wd})
}

// relDiags makes the spans' files relative to dir when under it.
func relDiags(ds []diag.Diagnostic, dir string) []diag.Diagnostic {
	rel := func(s *diag.Span) {
		if !filepath.IsAbs(s.File) {
			return
		}
		f := s.File
		if r, err := filepath.EvalSymlinks(f); err == nil {
			f = r
		}
		if r, err := filepath.Rel(dir, f); err == nil && !strings.HasPrefix(r, "..") {
			s.File = filepath.ToSlash(r)
		}
	}
	for i := range ds {
		rel(&ds[i].Span)
		ds[i].Related = slices.Clone(ds[i].Related)
		for j := range ds[i].Related {
			rel(&ds[i].Related[j])
		}
	}
	return ds
}

// labelSpan is the span of a "file:line" label.
func labelSpan(label string) diag.Span {
	file, line, _ := strings.Cut(label, ":")
	s := diag.Span{File: file}
	fmt.Sscan(line, &s.Line)
	return s
}

var codeArg = regexp.MustCompile(`^[EeWw]\d{4}$`)

// explainCode prints the catalogue's page of a code.
func explainCode(code string, w io.Writer) error {
	e, ok := docs.Lookup(code)
	if !ok {
		return fmt.Errorf("vuka explain: no code %s (the codes: %s)", strings.ToUpper(code), diag.DocsURL)
	}
	fmt.Fprintln(w, markdownText(e.Body))
	fmt.Fprintf(w, "See %s\n", diag.Code(e.Code).URL())
	return nil
}

// markdownText is a catalogue page for the terminal: code fences become
// indented blocks, headings and inline code marks go.
func markdownText(md string) string {
	var b strings.Builder
	fence := false
	for _, l := range strings.Split(strings.TrimRight(md, "\n"), "\n") {
		switch {
		case strings.HasPrefix(l, "```"):
			fence = !fence
			continue
		case fence:
			b.WriteString("    " + l + "\n")
			continue
		case strings.HasPrefix(l, "#"):
			l = strings.TrimSpace(strings.TrimLeft(l, "#"))
		}
		b.WriteString(l + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// colorFor reports whether w is a terminal to colour.
func colorFor(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && diag.ColorFor(f)
}

// lspFrom is a diagnostic for the editor: its code linking to its page,
// its message followed by what to do.
func lspFrom(r lspRange, d diag.Diagnostic) lspDiagnostic {
	d = docs.Fill([]diag.Diagnostic{d})[0]
	msg := d.Message
	if d.Help != "" {
		msg += "\nhelp: " + d.Help
	}
	if d.Suggest != "" {
		msg += "\nhelp: did you mean " + d.Suggest + "?"
	}
	ld := lspDiagnostic{Range: r, Severity: 1, Source: "vuka", Message: msg}
	if d.Severity == diag.Warning {
		ld.Severity = 2
	}
	if d.Code != "" {
		ld.Code, ld.CodeDescription = string(d.Code), &lspCodeDesc{Href: d.Code.URL()}
	}
	return ld
}

func lspFromError(r lspRange, e *transpile.Error) lspDiagnostic {
	return lspFrom(r, e.Diagnostic(""))
}

// offsetAt is the offset of a 1-based line and byte column in text.
func offsetAt(text []byte, line, col int) int {
	off := 0
	for l := 1; l < line && off < len(text); l++ {
		i := bytes.IndexByte(text[off:], '\n')
		if i < 0 {
			return len(text)
		}
		off += i + 1
	}
	return min(off+max(col-1, 0), len(text))
}
