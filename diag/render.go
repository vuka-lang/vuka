package diag

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Options says how Render prints.
type Options struct {
	Color   bool
	NoFrame bool // the location only, no code frame
	// NoFooter leaves out the lines naming the codes vuka explain explains.
	NoFooter bool
	Root     string // absolute spans under it are shown relative to it
	// Source is a file's text for its code frame (nil: read from disk; a
	// relative file is read relative to Root).
	Source func(file string) ([]byte, bool)
}

// ColorFor reports whether output to f should be coloured: a terminal,
// without NO_COLOR set or TERM=dumb (FORCE_COLOR forces it).
func ColorFor(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// Render prints the diagnostics, each as a code frame, then which codes
// `vuka explain` explains (unless NoFooter).
func Render(w io.Writer, ds []Diagnostic, o Options) error {
	var b bytes.Buffer
	for i := range ds {
		if i > 0 {
			b.WriteByte('\n')
		}
		render(&b, &ds[i], o)
	}
	if !o.NoFooter && !o.NoFrame {
		footer(&b, ds, o)
	}
	_, err := w.Write(b.Bytes())
	return err
}

// Footer prints which of the diagnostics' codes `vuka explain` explains, as
// Render does after them: nothing when they have no code.
func Footer(w io.Writer, ds []Diagnostic, o Options) error {
	var b bytes.Buffer
	footer(&b, ds, o)
	_, err := w.Write(b.Bytes())
	return err
}

func footer(b *bytes.Buffer, ds []Diagnostic, o Options) {
	codes := map[Code]bool{}
	for i := range ds {
		if ds[i].Code != "" {
			codes[ds[i].Code] = true
		}
	}
	if len(codes) == 0 {
		return
	}
	list := make([]string, 0, len(codes))
	for c := range codes {
		list = append(list, string(c))
	}
	sort.Strings(list)
	p := painter(o.Color)
	b.WriteByte('\n')
	if len(list) > 1 {
		fmt.Fprintf(b, "%s %s.\n", p(bold, "Some errors have detailed explanations:"), strings.Join(list, ", "))
		fmt.Fprintf(b, "%s\n", p(bold, "For more information about an error, try `vuka explain "+list[0]+"`."))
	} else {
		fmt.Fprintf(b, "%s\n", p(bold, "For more information about this error, try `vuka explain "+list[0]+"`."))
	}
}

const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	red    = "\x1b[1;31m"
	yellow = "\x1b[1;33m"
	blue   = "\x1b[1;34m"
)

func painter(on bool) func(style, s string) string {
	return func(style, s string) string {
		if !on || s == "" {
			return s
		}
		return style + s + reset
	}
}

type frameLine struct {
	n    int
	text string // tabs expanded
	from int    // display column of the span's start, 0-based
	to   int    // exclusive
}

func render(b *bytes.Buffer, d *Diagnostic, o Options) {
	p := painter(o.Color)
	sev, mark := red, "^"
	if d.Severity == Warning {
		sev = yellow
	}
	head := d.Severity.String()
	if d.Code != "" {
		head += "[" + string(d.Code) + "]"
	}
	msg, more, _ := strings.Cut(d.Message, "\n")
	fmt.Fprintf(b, "%s%s\n", p(sev, head), p(bold, ": "+msg))

	primary := frame(d.Span, o)
	related := make([]*frameLine, len(d.Related))
	width := 1
	grow := func(f *frameLine) {
		if f != nil && len(strconv.Itoa(f.n)) > width {
			width = len(strconv.Itoa(f.n))
		}
	}
	grow(primary)
	for i, s := range d.Related {
		related[i] = frame(s, o)
		grow(related[i])
	}
	pad := strings.Repeat(" ", width)
	gutter := p(blue, pad+" |")
	snippet := func(f *frameLine, label, style, mark string) {
		fmt.Fprintf(b, "%s\n", gutter)
		fmt.Fprintf(b, "%s %s\n", p(blue, fmt.Sprintf("%*d |", width, f.n)), f.text)
		under := strings.Repeat(" ", f.from) + strings.Repeat(mark, max(1, f.to-f.from))
		if label != "" {
			under += " " + label
		}
		fmt.Fprintf(b, "%s %s\n", gutter, p(style, under))
	}
	if loc := show(d.Span, o); loc != "" {
		fmt.Fprintf(b, "%s%s %s\n", pad, p(blue, "-->"), loc)
	}
	if primary != nil {
		snippet(primary, d.Span.Label, sev, mark)
	} else if d.Span.Label != "" {
		fmt.Fprintf(b, "%s %s %s\n", pad, p(blue, "="), d.Span.Label)
	}
	for i, s := range d.Related {
		loc := show(s, o)
		if related[i] != nil {
			fmt.Fprintf(b, "%s%s %s\n", pad, p(blue, ":::"), loc)
			snippet(related[i], s.Label, blue, "-")
			continue
		}
		switch {
		case loc != "" && s.Label != "":
			fmt.Fprintf(b, "%s%s %s: %s\n", pad, p(blue, ":::"), loc, s.Label)
		case loc != "":
			fmt.Fprintf(b, "%s%s %s\n", pad, p(blue, ":::"), loc)
		case s.Label != "":
			fmt.Fprintf(b, "%s %s %s\n", pad, p(blue, "="), s.Label)
		}
	}
	note := func(kind, text string) {
		lines := strings.Split(text, "\n")
		fmt.Fprintf(b, "%s %s %s\n", pad, p(blue, "="), p(bold, kind+":")+" "+lines[0])
		indent := pad + "   " + strings.Repeat(" ", len(kind)+1)
		for _, l := range lines[1:] {
			fmt.Fprintf(b, "%s %s\n", indent, l)
		}
	}
	if primary != nil && (d.Help != "" || d.Suggest != "" || more != "") {
		fmt.Fprintf(b, "%s\n", gutter)
	}
	if more != "" {
		var ls []string
		for _, l := range strings.Split(more, "\n") {
			ls = append(ls, strings.TrimSpace(l))
		}
		note("note", strings.Join(ls, "\n"))
	}
	if d.Help != "" {
		note("help", d.Help)
	}
	if d.Suggest != "" {
		note("help", "did you mean "+d.Suggest+"?")
	}
	if o.NoFrame && d.Code != "" {
		note("note", "vuka explain "+string(d.Code)+" ("+d.Code.URL()+")")
	}
}

// show is a span's location, relative to o.Root when under it.
func show(s Span, o Options) string {
	if s.File == "" {
		return ""
	}
	if o.Root != "" && filepath.IsAbs(s.File) {
		if rel, err := filepath.Rel(o.Root, s.File); err == nil && !strings.HasPrefix(rel, "..") {
			s.File = filepath.ToSlash(rel)
		}
	}
	return s.String()
}

// frame is the source line a span is on, nil without one.
func frame(s Span, o Options) *frameLine {
	if o.NoFrame || s.File == "" || s.Line == 0 {
		return nil
	}
	src, ok := source(s.File, o)
	if !ok {
		return nil
	}
	lines := strings.Split(string(src), "\n")
	if s.Line > len(lines) {
		return nil
	}
	raw := strings.TrimRight(lines[s.Line-1], "\r")
	col := func(c int) int { // a 1-based byte column → a display column
		n := 0
		for i, r := range raw {
			if i >= c-1 {
				break
			}
			if r == '\t' {
				n += 4
			} else {
				n++
			}
		}
		return n
	}
	f := &frameLine{n: s.Line, text: strings.ReplaceAll(raw, "\t", "    ")}
	if s.Col == 0 {
		lead := len(raw) - len(strings.TrimLeft(raw, " \t"))
		f.from, f.to = col(lead+1), col(len(raw)+1)
		return f
	}
	f.from = col(s.Col)
	f.to = f.from + 1
	if s.EndCol > s.Col {
		f.to = col(s.EndCol)
	} else if end := tokenEnd(raw, s.Col-1); end > s.Col-1 {
		f.to = col(end + 1)
	}
	return f
}

// tokenEnd is the byte offset just past the word (or @attribute, dotted
// name, <tag) at i.
func tokenEnd(line string, i int) int {
	j := i
	if j < len(line) && (line[j] == '@' || line[j] == '<') {
		j++
	}
	for j < len(line) {
		c := line[j]
		if isWord(c) || c == '_' || c == '.' && j+1 < len(line) && isWord(line[j+1]) {
			j++
			continue
		}
		break
	}
	if j == i {
		return i + 1
	}
	return j
}

func isWord(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}

func source(file string, o Options) ([]byte, bool) {
	if o.Source != nil {
		if b, ok := o.Source(file); ok {
			return b, true
		}
	}
	p := file
	if !filepath.IsAbs(p) && o.Root != "" {
		p = filepath.Join(o.Root, p)
	}
	b, err := os.ReadFile(p)
	return b, err == nil
}
