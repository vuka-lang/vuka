package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// modulePath is the module a playground project is.
const modulePath = "example.com/play"

const (
	maxFiles = 50
	maxBytes = 256 << 10
)

// Span and Diagnostic are the shape vuka build -diag=json writes and the
// playground's WASM side returns.
type Span struct {
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Col    int    `json:"col,omitempty"`
	EndCol int    `json:"endCol,omitempty"`
}

type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Span     Span   `json:"span"`
	Help     string `json:"help,omitempty"`
	Suggest  string `json:"suggest,omitempty"`
	URL      string `json:"url,omitempty"`
}

// knownModules are the modules a project may import besides std and itself.
var knownModules = []string{
	"github.com/vuka-lang/ui/v2",
	"github.com/vuka-lang/web/v2",
	"github.com/vuka-lang/orm/v2",
	"github.com/vuka-lang/vuka/v2",
}

const knownMessage = "the playground knows std, ui, web and orm"

// checkFiles says what is wrong with a project's files before anything
// builds them: their number, size, paths and imports.
func checkFiles(files map[string]string) ([]Diagnostic, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("the project has no files")
	}
	if len(files) > maxFiles {
		return nil, fmt.Errorf("the project has %d files; the runner takes %d", len(files), maxFiles)
	}
	total := 0
	for p, src := range files {
		total += len(p) + len(src)
	}
	if total > maxBytes {
		return nil, fmt.Errorf("the project is %d KB; the runner takes %d KB", total>>10, maxBytes>>10)
	}
	var ds []Diagnostic
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if msg := badPath(p); msg != "" {
			ds = append(ds, Diagnostic{Code: "E0003", Severity: "error", Message: msg, Span: Span{File: p}})
			continue
		}
		ds = append(ds, badImports(p, files[p])...)
	}
	return ds, nil
}

var allowedName = regexp.MustCompile(`^[\w.-]+\.(vuka|vuka\.html|go|css)$`)

func badPath(p string) string {
	switch {
	case p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\"):
		return fmt.Sprintf("%q isn't a path in the project", p)
	case path.Clean(p) != p || strings.HasPrefix(p, "../"):
		return fmt.Sprintf("%q isn't a clean path in the project", p)
	case p == "build" || strings.HasPrefix(p, "build/"):
		return fmt.Sprintf("%q: build/ is where vuka build writes", p)
	}
	for _, part := range strings.Split(p, "/") {
		if strings.HasPrefix(part, ".") || strings.HasPrefix(part, "_") {
			return fmt.Sprintf("%q: a project file's folders and name don't start with . or _", p)
		}
	}
	if !allowedName.MatchString(path.Base(p)) {
		return fmt.Sprintf("%q: the runner takes .vuka, .vuka.html, .go and .css files", p)
	}
	return ""
}

var scriptGo = regexp.MustCompile(`(?s)<script\s+lang="go"\s*>(.*?)</script>`)

// badImports is a diagnostic for each import of src the image's module
// cache doesn't hold.
func badImports(p, src string) []Diagnostic {
	offset := 0
	switch {
	case strings.HasSuffix(p, ".vuka.html"):
		m := scriptGo.FindStringSubmatchIndex(src)
		if m == nil {
			return nil
		}
		offset = strings.Count(src[:m[2]], "\n")
		src = src[m[2]:m[3]]
	case strings.HasSuffix(p, ".vuka"), strings.HasSuffix(p, ".go"):
	default:
		return nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, p, src, parser.ImportsOnly)
	if err != nil || f == nil {
		return nil
	}
	var ds []Diagnostic
	for _, im := range f.Imports {
		ip, err := strconv.Unquote(im.Path.Value)
		if err != nil || known(ip) {
			continue
		}
		pos := fset.Position(im.Path.Pos())
		msg := "cannot import " + ip + ": " + knownMessage
		ds = append(ds, Diagnostic{Code: "E0002", Severity: "error", Message: msg,
			Span: Span{File: p, Line: pos.Line + offset, Col: pos.Column, EndCol: pos.Column + len(im.Path.Value)}})
	}
	return ds
}

func known(ip string) bool {
	first, _, _ := strings.Cut(ip, "/")
	if !strings.Contains(first, ".") {
		return true
	}
	for _, m := range append([]string{modulePath}, knownModules...) {
		if ip == m || strings.HasPrefix(ip, m+"/") {
			return true
		}
	}
	return false
}

// template is the go.mod and go.sum every project builds with: the docs'
// pinned module (docs/testdata/gomode) renamed, vuka/v2 replaced by the
// source the image carries.
type template struct {
	gomod, gosum []byte
}

func loadTemplate(gomod, vukaSrc string) (*template, error) {
	mod, err := os.ReadFile(gomod)
	if err != nil {
		return nil, err
	}
	sum, err := os.ReadFile(filepath.Join(filepath.Dir(gomod), "go.sum"))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	sc := bufio.NewScanner(bytes.NewReader(mod))
	for sc.Scan() {
		line := sc.Text()
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "module "):
			line = "module " + modulePath
		case strings.HasPrefix(t, "tool "), strings.HasPrefix(t, "//"):
			continue
		case strings.HasPrefix(t, "replace github.com/vuka-lang/vuka/v2 "):
			if vukaSrc == "" {
				continue
			}
			line = "replace github.com/vuka-lang/vuka/v2 => " + vukaSrc
		}
		out.WriteString(line + "\n")
	}
	return &template{gomod: out.Bytes(), gosum: sum}, nil
}

// write puts the project and the template's go.mod and go.sum in dir.
func (t *template) write(dir string, files map[string]string) error {
	for p, src := range files {
		f := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), t.gomod, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "go.sum"), t.gosum, 0o644)
}

// buildDiagnostics are the diagnostics vuka build -diag=json wrote among
// its output: each a line holding a JSON array.
func buildDiagnostics(log []byte) []Diagnostic {
	var out []Diagnostic
	for _, line := range bytes.Split(log, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) < 2 || line[0] != '[' {
			continue
		}
		var ds []Diagnostic
		if json.Unmarshal(line, &ds) == nil {
			for _, d := range ds {
				if d.Code != "" {
					out = append(out, d)
				}
			}
		}
	}
	return out
}

// plainLog is the build log without the diagnostics' JSON lines.
func plainLog(log []byte) string {
	var b strings.Builder
	for _, line := range bytes.SplitAfter(log, []byte("\n")) {
		t := bytes.TrimSpace(line)
		if len(t) >= 2 && t[0] == '[' && json.Valid(t) {
			continue
		}
		b.Write(line)
	}
	return b.String()
}
