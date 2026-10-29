package main

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// The editor on web.Asset: the E0429s and W0404s of a .vuka or .vuka.html
// file, the quick fix linking a hard-coded path with web.Asset, and inside
// a web.Asset("…") literal the static files' paths as completions, a file's
// size (and an image's dimensions) on hover, and the file as its definition.

// staticEditorDiags are the static-file diagnostics of the file at path.
func staticEditorDiags(root, path string, text []byte) []any {
	if root == "" || !sourceFile(filepath.Base(path)) || filepath.Ext(path) == ".go" {
		return nil
	}
	mounts := editorMounts(root)
	if !slices.ContainsFunc(mounts, staticMount.embedded) {
		return nil
	}
	var out []any
	for _, d := range fileStaticDiags(path, text, mounts, staticURLs(mounts), true) {
		s := offsetAt(text, d.Span.Line, d.Span.Col)
		e := offsetAt(text, d.Span.Line, d.Span.EndCol)
		out = append(out, lspFrom(lspRange{positionOf(text, s), positionOf(text, e)}, d))
	}
	return out
}

// staticPathActions are the quick fixes writing the hard-coded static paths
// the request's range touches with web.Asset.
func staticPathActions(r actionRequest) []any {
	root, _, err := load.ModuleRoot(filepath.Dir(r.path))
	if err != nil {
		return nil
	}
	mounts := editorMounts(root)
	attrs := staticAttrs(r.path, r.src, mounts)
	var out []any
	for _, a := range attrs {
		if !r.touches(a.start, a.end) {
			continue
		}
		name := webName(r.src)
		es := []edit{{a.start, a.end, assetExpr(name, a.attr, a.value, mounts)}}
		if !webImportRe.Match(r.src) {
			for _, e := range addImports(transpile.HeaderView(r.src), []importSpec{{path: webPath}}) {
				es = append(es, edit{e.start, e.end, e.text})
			}
		}
		act := codeAction{Title: "Link " + a.value + " with web.Asset", Kind: "quickfix", IsPreferred: true,
			Edit: change{{path: r.path, old: r.src, edits: es}}.workspaceEdit(r.uri)}
		if d := r.diag("W0404"); d != nil {
			act.Diagnostics = []map[string]any{d}
		}
		out = append(out, act)
	}
	return out
}

// openAssetLit is a web.Asset literal being written up to the cursor.
var openAssetLit = regexp.MustCompile(`\.(?:AssetURL|Asset)\(\s*(?:[A-Za-z_][\w.]*\s*,\s*)?"([^"\n]*)$`)

// assetRequest answers completion inside a web.Asset literal, and hover and
// definition on one; nil when off isn't in one. p.mu must be held.
func (p *proxy) assetRequest(method string, f *vfile, off int) func() any {
	root, text, source := p.modRoot, f.text(), f.source
	if root == "" || off > len(text) || !strings.Contains(string(text), "Asset") {
		return nil
	}
	switch method {
	case "textDocument/completion":
		lineStart := strings.LastIndexByte(string(text[:off]), '\n') + 1
		m := openAssetLit.FindSubmatchIndex(text[lineStart:off])
		if m == nil {
			return nil
		}
		start, typed := lineStart+m[2], string(text[lineStart+m[2]:off])
		return func() any {
			urls := staticURLs(editorMounts(root))
			items := []any{}
			for _, u := range slices.Sorted(func(yield func(string) bool) {
				for u := range urls {
					if !yield(u) {
						return
					}
				}
			}) {
				if !strings.HasPrefix(u, typed) && !strings.HasPrefix(u, "/"+strings.TrimPrefix(typed, "/")) {
					continue
				}
				items = append(items, map[string]any{
					"label": u, "kind": 17, "detail": relTo(root, urls[u]),
					"textEdit": map[string]any{"range": lspRange{positionOf(text, start), positionOf(text, off)}, "newText": u},
				})
			}
			return map[string]any{"isIncomplete": false, "items": items}
		}
	case "textDocument/hover", "textDocument/definition", "textDocument/declaration":
		var at *assetLit
		for _, l := range assetLits(source, text) {
			if off >= l.start && off <= l.end {
				at = &l
				break
			}
		}
		if at == nil {
			return nil
		}
		return func() any {
			u := at.path
			if i := strings.IndexAny(u, "?#"); i >= 0 {
				u = u[:i]
			}
			file := staticURLs(editorMounts(root))[u]
			if file == "" {
				return nil
			}
			if method != "textDocument/hover" {
				return []any{map[string]any{"uri": pathToURI(p.display(file)), "range": lspRange{}}}
			}
			return map[string]any{
				"contents": map[string]any{"kind": "markdown", "value": assetHover(root, file)},
				"range":    lspRange{positionOf(text, at.start), positionOf(text, at.end)},
			}
		}
	}
	return nil
}

// assetHover is a static file's path under the module, size, and an image's
// dimensions.
func assetHover(root, file string) string {
	s := "`" + relTo(root, file) + "`"
	fi, err := os.Stat(file)
	if err != nil {
		return s
	}
	s += ", " + byteSize(fi.Size())
	if r, err := os.Open(file); err == nil {
		if c, _, err := image.DecodeConfig(r); err == nil {
			s += fmt.Sprintf(", %d×%d", c.Width, c.Height)
		}
		r.Close()
	}
	return s
}

func byteSize(n int64) string {
	switch {
	case n >= 1<<20:
		return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MB"
	case n >= 1<<10:
		return strconv.FormatFloat(float64(n)/(1<<10), 'f', 1, 64) + " KB"
	}
	return strconv.FormatInt(n, 10) + " B"
}

func relTo(root, file string) string {
	if r, err := filepath.Rel(root, file); err == nil && !strings.HasPrefix(r, "..") {
		return filepath.ToSlash(r)
	}
	return file
}
