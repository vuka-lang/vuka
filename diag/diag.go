// Package diag is the shape of every diagnostic the vuka toolchain reports:
// the transpiler, vuka vet, the editor and the libraries' boot-time checks.
// A diagnostic has a stable code (E0101, W0301), a severity, a primary span,
// a message, and what to do about it; Render prints it as a code frame and
// its JSON form is what editors read. Codes are explained by `vuka explain`
// and on the docs site (docs/errors).
package diag

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Code is a diagnostic's stable code: E for an error, W for a warning, then
// four digits whose first two name the area (01 syntax and JSX, 02
// decorators and attributes, 03 islands and live pages, 04 routes and the web
// app, 05 data, 06 frontend and toolchain, 00 the compiler itself).
type Code string

// URL is where the docs site explains the code.
func (c Code) URL() string { return DocsURL + string(c) }

// DocsURL is the docs site's errors section.
const DocsURL = "https://vuka-lang.github.io/errors/"

// Severity is how bad a diagnostic is.
type Severity int

const (
	Error Severity = iota
	Warning
)

func (s Severity) String() string {
	if s == Warning {
		return "warning"
	}
	return "error"
}

func (s Severity) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

func (s *Severity) UnmarshalJSON(b []byte) error {
	var v string
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*s = Error
	if v == "warning" {
		*s = Warning
	}
	return nil
}

// Span is a place in a source file. Line and Col are 1-based (Col counts
// bytes); 0 means unknown. EndCol, when past Col, ends the span on the same
// line (exclusive). Label says what the span shows.
type Span struct {
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Col    int    `json:"col,omitempty"`
	EndCol int    `json:"endCol,omitempty"`
	Label  string `json:"label,omitempty"`
}

func (s Span) String() string {
	switch {
	case s.File == "":
		return ""
	case s.Line == 0:
		return s.File
	case s.Col == 0:
		return fmt.Sprintf("%s:%d", s.File, s.Line)
	}
	return fmt.Sprintf("%s:%d:%d", s.File, s.Line, s.Col)
}

// Diagnostic is one problem and what to do about it.
type Diagnostic struct {
	Code     Code     `json:"code"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Span     Span     `json:"span"`
	Related  []Span   `json:"related,omitempty"`
	Help     string   `json:"help,omitempty"`
	Suggest  string   `json:"suggest,omitempty"` // "did you mean …"
}

// New is an error diagnostic.
func New(code Code, format string, args ...any) *Diagnostic {
	return &Diagnostic{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Warn is a warning diagnostic.
func Warn(code Code, format string, args ...any) *Diagnostic {
	return &Diagnostic{Code: code, Severity: Warning, Message: fmt.Sprintf(format, args...)}
}

// At sets the primary span.
func (d *Diagnostic) At(s Span) *Diagnostic { d.Span = s; return d }

// AtLine sets the primary span to a file and line.
func (d *Diagnostic) AtLine(file string, line int) *Diagnostic {
	d.Span = Span{File: file, Line: line}
	return d
}

// WithHelp sets what to do about it.
func (d *Diagnostic) WithHelp(format string, args ...any) *Diagnostic {
	d.Help = fmt.Sprintf(format, args...)
	return d
}

// WithSuggest sets the "did you mean".
func (d *Diagnostic) WithSuggest(s string) *Diagnostic { d.Suggest = s; return d }

// WithRelated adds a secondary span.
func (d *Diagnostic) WithRelated(s Span) *Diagnostic {
	d.Related = append(d.Related, s)
	return d
}

// Error is the diagnostic rendered plainly, without a code frame.
func (d *Diagnostic) Error() string {
	var b bytes.Buffer
	render(&b, d, Options{NoFrame: true})
	return strings.TrimRight(b.String(), "\n")
}

// Lister is an error holding several diagnostics.
type Lister interface {
	Diagnostics() []Diagnostic
}

// List is several diagnostics as one error.
type List []Diagnostic

func (l List) Diagnostics() []Diagnostic { return l }

func (l List) Error() string {
	var b strings.Builder
	for i := range l {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(l[i].Error())
	}
	return b.String()
}

// From is the diagnostics err holds, through errors.Join; rest is the parts
// of err that aren't diagnostics.
func From(err error) (ds []Diagnostic, rest []error) {
	var walk func(error)
	walk = func(err error) {
		switch e := err.(type) {
		case nil:
		case *Diagnostic:
			ds = append(ds, *e)
		case Lister:
			ds = append(ds, e.Diagnostics()...)
		case interface{ Unwrap() []error }:
			for _, x := range e.Unwrap() {
				walk(x)
			}
		default:
			var d *Diagnostic
			var l Lister
			switch {
			case errors.As(err, &d) && err.Error() == d.Error():
				ds = append(ds, *d)
			case errors.As(err, &l) && err.Error() == l.(error).Error():
				ds = append(ds, l.Diagnostics()...)
			default:
				rest = append(rest, err)
			}
		}
	}
	walk(err)
	return ds, rest
}

// Of is the code of the first diagnostic err holds, "" when none.
func Of(err error) Code {
	if ds, _ := From(err); len(ds) > 0 {
		return ds[0].Code
	}
	var d *Diagnostic
	if errors.As(err, &d) {
		return d.Code
	}
	return ""
}

// JSON is the diagnostics as a JSON array, for editors and tools.
func JSON(ds []Diagnostic) []byte {
	type withURL struct {
		Diagnostic
		URL string `json:"url,omitempty"`
	}
	out := make([]withURL, len(ds))
	for i, d := range ds {
		out[i] = withURL{d, ""}
		if d.Code != "" {
			out[i].URL = d.Code.URL()
		}
	}
	b, _ := json.Marshal(out)
	return b
}
