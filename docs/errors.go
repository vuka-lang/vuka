// Package docs holds the diagnostic catalogue: docs/errors/<code>.md, one
// page per code, rendered by the docs site and printed by `vuka explain`.
package docs

import (
	"embed"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
)

//go:embed errors/[EW]*.md
var errorsFS embed.FS

// Entry is one code's page.
type Entry struct {
	Code  string
	Title string
	Area  string
	Help  string // the one-line help a diagnostic without its own shows
	Body  string // the Markdown after the front matter
}

// Areas name the codes' areas by their first two digits.
var Areas = map[string]string{
	"00": "The compiler",
	"01": "Syntax, JSX and the language",
	"02": "Decorators and attributes",
	"03": "Islands and live pages",
	"04": "Routes and the web app",
	"05": "Data and the ORM",
	"06": "Frontend and toolchain",
	"07": "Folder routes",
}

// Errors is every entry, by code.
func Errors() []Entry {
	names, _ := fs.Glob(errorsFS, "errors/*.md")
	out := make([]Entry, 0, len(names))
	for _, n := range names {
		b, err := errorsFS.ReadFile(n)
		if err != nil {
			continue
		}
		e := parseEntry(string(b))
		if e.Code == "" {
			e.Code = strings.TrimSuffix(path.Base(n), ".md")
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// Lookup is the entry of code.
func Lookup(code string) (Entry, bool) {
	b, err := errorsFS.ReadFile("errors/" + strings.ToUpper(code) + ".md")
	if err != nil {
		return Entry{}, false
	}
	return parseEntry(string(b)), true
}

func parseEntry(s string) Entry {
	var e Entry
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if rest, ok := strings.CutPrefix(s, "---\n"); ok {
		if head, body, ok := strings.Cut(rest, "\n---\n"); ok {
			for _, l := range strings.Split(head, "\n") {
				k, v, _ := strings.Cut(l, ":")
				v = yamlScalar(strings.TrimSpace(v))
				switch strings.TrimSpace(k) {
				case "code":
					e.Code = v
				case "title":
					e.Title = v
				case "area":
					e.Area = v
				case "help":
					e.Help = v
				}
			}
			s = body
		}
	}
	e.Body = strings.TrimLeft(styleMarks.ReplaceAllString(s, ""), "\n")
	return e
}

// styleMarks are the lines of a page's ::: vuka and ::: go containers,
// which only the site reads: the terminal shows both samples.
var styleMarks = regexp.MustCompile(`(?m)^:::( vuka| go)?[ \t]*\n`)

// yamlScalar is a front-matter value: plain, 'single' or "double" quoted.
func yamlScalar(v string) string {
	switch {
	case len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'':
		return strings.ReplaceAll(v[1:len(v)-1], "''", "'")
	case len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"':
		if u, err := strconv.Unquote(v); err == nil {
			return u
		}
		return v[1 : len(v)-1]
	}
	return v
}

// Fill gives each diagnostic without its own help its code's.
func Fill(ds []diag.Diagnostic) []diag.Diagnostic {
	for i := range ds {
		if ds[i].Help != "" || ds[i].Code == "" {
			continue
		}
		if e, ok := Lookup(string(ds[i].Code)); ok {
			ds[i].Help = e.Help
		}
	}
	return ds
}
