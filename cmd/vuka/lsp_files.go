package main

import (
	"bytes"
	"cmp"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/vuka-lang/vuka/transpile"
)

// fileLink is a vuka.File literal in the editor's text: the path inside its
// quotes, at start:end.
type fileLink struct {
	start, end int
	ref        transpile.FileRef
}

// fileLinks are f's vuka.File literals, mapped onto the editor's text; one
// whose text changed since the Go was generated is left out.
func (f *vfile) fileLinks() []fileLink {
	var links []fileLink
	text := f.text()
	for _, r := range f.files {
		if r.End-r.Off < 2 || r.End > len(f.from) {
			continue
		}
		s, e := f.curOff(r.Off+1), f.curOff(r.End-1)
		if s < 1 || e >= len(text) || !bytes.Equal(text[s-1:e+1], f.from[r.Off:r.End]) {
			continue
		}
		links = append(links, fileLink{s, e, r})
	}
	return links
}

// fileAt is the vuka.File literal at off, quotes included.
func (f *vfile) fileAt(off int) (fileLink, bool) {
	for _, l := range f.fileLinks() {
		if off >= l.start-1 && off <= l.end+1 {
			return l, true
		}
	}
	return fileLink{}, false
}

// fileRequest answers definition, declaration and hover on a vuka.File
// literal itself; nil when off isn't in one. p.mu must be held.
func (p *proxy) fileRequest(method string, f *vfile, off int) func() any {
	if method != "textDocument/definition" && method != "textDocument/declaration" && method != "textDocument/hover" {
		return nil
	}
	l, ok := f.fileAt(off)
	if !ok {
		return nil
	}
	modRoot, text := p.modRoot, f.text()
	return func() any {
		if method == "textDocument/hover" {
			return map[string]any{
				"contents": map[string]any{"kind": "markdown", "value": fileHover(l.ref, modRoot)},
				"range":    lspRange{positionOf(text, l.start), positionOf(text, l.end)},
			}
		}
		return []any{map[string]any{"uri": pathToURI(p.display(l.ref.Path)), "range": templTarget(l.ref)}}
	}
}

// documentLinks are f's vuka.File literals as document links.
func (p *proxy) documentLinks(f *vfile) []any {
	text := f.text()
	links := []any{}
	for _, l := range f.fileLinks() {
		links = append(links, map[string]any{
			"range":  lspRange{positionOf(text, l.start), positionOf(text, l.end)},
			"target": pathToURI(p.display(l.ref.Path)),
		})
	}
	return links
}

// withFileLinks adds f's vuka.File links to gopls's links for its Go, keeping
// only those that mapped onto the source.
func (p *proxy) withFileLinks(v any, f *vfile) any {
	out := p.documentLinks(f)
	list, _ := v.([]any)
	for _, l := range list {
		if m, ok := l.(map[string]any); ok && m["range"] != nil {
			out = append(out, m)
		}
	}
	return out
}

func fileHover(r transpile.FileRef, modRoot string) string {
	shown := r.Path
	if rel, err := filepath.Rel(modRoot, r.Path); err == nil && modRoot != "" && !strings.HasPrefix(rel, "..") {
		shown = filepath.ToSlash(rel)
	}
	var b strings.Builder
	b.WriteString("`" + shown + "`")
	if len(r.Components) > 0 {
		b.WriteString("\n\n```templ\n")
		for _, c := range r.Components {
			params := make([]string, len(c.Params))
			for i, p := range c.Params {
				params[i] = cmp.Or(p, "_")
			}
			b.WriteString("templ " + c.Name + "(" + strings.Join(params, ", ") + ")\n")
		}
		b.WriteString("```")
	}
	return b.String()
}

var templDecl = regexp.MustCompile(`(?m)^templ\s+([A-Za-z_]\w*)\s*\(`)

// templTarget is where a definition of a vuka.File lands: the component of a
// .templ file that has one, else the file's start.
func templTarget(r transpile.FileRef) lspRange {
	if len(r.Components) != 1 {
		return lspRange{}
	}
	src, err := os.ReadFile(r.Path)
	if err != nil {
		return lspRange{}
	}
	for _, m := range templDecl.FindAllSubmatchIndex(src, -1) {
		if string(src[m[2]:m[3]]) == r.Components[0].Name {
			return lspRange{positionOf(src, m[2]), positionOf(src, m[3])}
		}
	}
	return lspRange{}
}
