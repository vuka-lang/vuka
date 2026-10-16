package main

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// The editor's view of @ui.Island decorators: hover names the component file
// (inferred or written) and its framework, definition opens it, and a
// decorator whose file is missing or ambiguous is a diagnostic — read from
// the text, as the build reads it, without waiting for one.

// frontendRoots caches findFrontendRoot per module root for a moment: it
// walks the sources.
var frontendRoots struct {
	sync.Mutex
	m map[string]frontendRootAt
}

type frontendRootAt struct {
	dir string
	at  time.Time
}

func cachedFrontendRoot(root string) string {
	frontendRoots.Lock()
	defer frontendRoots.Unlock()
	if e, ok := frontendRoots.m[root]; ok && time.Since(e.at) < 3*time.Second {
		return e.dir
	}
	dir := findFrontendRoot(root)
	if frontendRoots.m == nil {
		frontendRoots.m = map[string]frontendRootAt{}
	}
	frontendRoots.m[root] = frontendRootAt{dir, time.Now()}
	return dir
}

// islandDecos are the @ui.Island decorators of the .vuka text at path, each
// with its file located when locate is set.
func islandDecos(root, path string, text []byte, locate bool) []*islandComp {
	if root == "" || !strings.HasSuffix(path, ".vuka") || !bytes.Contains(text, []byte("Island")) {
		return nil
	}
	pa := &pageAssets{root: root}
	pa.readIslands(path, text)
	var out []*islandComp
	var fr string
	for _, ic := range pa.islands {
		if ic.imported {
			continue
		}
		if locate {
			if fr == "" {
				fr = cachedFrontendRoot(root)
			}
			ic.locate(root, fr)
		}
		out = append(out, ic)
	}
	return out
}

// islandIssue is what is wrong with an island's file, "" when nothing.
func islandIssue(ic *islandComp) string {
	if ic.problem != "" {
		return ic.problem
	}
	if fi, err := os.Stat(ic.src); err != nil || fi.IsDir() {
		return fmt.Sprintf("component %s not found", ic.rel)
	}
	return ""
}

// islandDiags are the diagnostics of text's @ui.Island decorators.
func islandDiags(root, path string, text []byte) []any {
	var out []any
	for _, ic := range islandDecos(root, path, text, true) {
		if msg := islandIssue(ic); msg != "" {
			out = append(out, lspDiagnostic{Range: lspRange{positionOf(text, ic.off), positionOf(text, ic.end)}, Severity: 1, Source: "vuka",
				Message: fmt.Sprintf("island %s: %s", ic.name, msg)})
		}
		if a := ic.routeAdvice(); a != "" {
			out = append(out, lspDiagnostic{Range: lspRange{positionOf(text, ic.routeOff), positionOf(text, ic.routeAt)}, Severity: 2, Source: "vuka", Message: a})
		}
	}
	return out
}

// islandRequest answers hover and definition on an @ui.Island decorator;
// nil when off isn't on one. p.mu must be held.
func (p *proxy) islandRequest(method string, f *vfile, off int) func() any {
	if method != "textDocument/definition" && method != "textDocument/declaration" && method != "textDocument/hover" {
		return nil
	}
	root, text := p.modRoot, f.text()
	if !bytes.Contains(text, []byte("Island")) {
		return nil
	}
	var at *islandComp
	for _, ic := range islandDecos(root, f.source, text, false) {
		if off >= ic.off && off <= ic.end {
			at = ic
		}
	}
	if at == nil {
		return nil
	}
	return func() any {
		at.locate(root, cachedFrontendRoot(root))
		issue := islandIssue(at)
		if method != "textDocument/hover" {
			if issue != "" {
				return nil
			}
			return []any{map[string]any{"uri": pathToURI(p.display(at.src)), "range": lspRange{}}}
		}
		how := "written"
		if at.infer {
			how = "inferred: islands/[<module>/]<Type>.<ext> under the frontend root"
		}
		var b strings.Builder
		fmt.Fprintf(&b, "island `%s`", at.name)
		if at.problem == "" {
			fmt.Fprintf(&b, ": `%s` (%s)", at.rel, at.framework)
		}
		fmt.Fprintf(&b, "\n\nThe path is %s.", how)
		if at.layout {
			b.WriteString("\n\nIts `ui.Layout` is the layout of the page the type backs when it is routed as one (`@web.Live`, `@web.Get`), over `web.Layout` and `web.LiveLayout`; placed inside another page, the island ignores it.")
			if at.route == "" {
				b.WriteString(" This type has no route decorator here.")
			}
		}
		if issue != "" {
			fmt.Fprintf(&b, "\n\n**Warning:** %s", issue)
		}
		return map[string]any{
			"contents": map[string]any{"kind": "markdown", "value": b.String()},
			"range":    lspRange{positionOf(text, at.off), positionOf(text, at.end)},
		}
	}
}

var hookRe = regexp.MustCompile(`func\s*\(\s*\w*\s*\*?\s*(\w+)\s*\)\s*$`)

// islandHookNote is what hover adds on an island page's Mount or Changed
// method name at off: they are hooks the server runs, not methods the
// component calls.
func islandHookNote(root, path string, text []byte, off int) string {
	s, e := off, off
	for s > 0 && isIdentByte(text[s-1]) {
		s--
	}
	for e < len(text) && isIdentByte(text[e]) {
		e++
	}
	name := string(text[s:e])
	if name != "Mount" && name != "Changed" {
		return ""
	}
	m := hookRe.FindSubmatch(text[max(0, s-200):s])
	if m == nil {
		return ""
	}
	typ := string(m[1])
	found := false
	for _, ic := range islandDecos(root, path, text, false) {
		found = found || ic.name == typ
	}
	if !found {
		return ""
	}
	if name == "Mount" {
		return "Lifecycle hook: the server runs Mount when the page is opened, before it renders. The component can't call it (it isn't on live)."
	}
	return "Lifecycle hook: the server runs Changed after the component sets a bind: field (live.<field>.set(v)) and the bound fields are set. The component can't call it (it isn't on live)."
}

// withNote adds note to a hover result.
func withNote(v any, note string) any {
	h, ok := v.(map[string]any)
	if !ok || h == nil {
		return map[string]any{"contents": map[string]any{"kind": "markdown", "value": note}}
	}
	if c, ok := h["contents"].(map[string]any); ok {
		if s, ok := c["value"].(string); ok {
			c["value"] = s + "\n\n---\n\n" + note
			return h
		}
	}
	h["contents"] = map[string]any{"kind": "markdown", "value": note}
	return h
}
