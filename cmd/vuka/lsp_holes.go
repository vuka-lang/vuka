package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Hover on the names of a .vuka.html file's markup — what a hole reads (a
// prop, server state, browser state, a local) and where it is set — and on
// on… attributes: where the handler runs. shapeRequest also routes the
// requests answered by lsp_tags.go and lsp_routes.go; anything they don't
// answer goes on to gopls.

// shapeRequest answers, from the source, the requests on a .vuka or
// .vuka.html file that vuka lsp has its own answer for; false leaves the
// request to gopls, with a note for its hover: what a name of the markup
// reads, or a component tag's props.
func (p *proxy) shapeRequest(m *rpcMsg, path string) (bool, string) {
	reply := func(result any) {
		_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
	}
	if m.Method == "textDocument/codeLens" {
		reply(p.codeLenses(path))
		return true, ""
	}
	switch m.Method {
	case "textDocument/hover", "textDocument/completion", "textDocument/definition", "textDocument/references":
	default:
		return false, ""
	}
	var params struct {
		Position lspPosition `json:"position"`
	}
	if json.Unmarshal(m.Params, &params) != nil {
		return false, ""
	}
	src := p.sourceText(path)
	off := offsetOf(src, params.Position)
	html := strings.HasSuffix(path, ".vuka.html")
	switch m.Method {
	case "textDocument/hover":
		if !html {
			return false, ""
		}
		s := scanShape(src)
		if e, ok := s.eventAt(off); ok && off <= e.attr.end {
			// gopls has nothing on an attribute's name; an element's
			// event gets its MDN description too.
			doc := s.eventDoc(e)
			if !isComponentTag(e.tag.name) {
				if d := attrDoc(e.tag.name, e.attr.name); d != "" {
					doc += "\n\n" + d
				}
			}
			reply(markdownHover(doc, src, e.attr.start, e.attr.end))
			return true, ""
		}
		if r, ok := s.refAt(off); ok {
			if r.kind == refComponent {
				return false, p.tagNote(path, src, s, off)
			}
			return false, s.refDoc(path, r)
		}
	case "textDocument/completion":
		if !html {
			return false, ""
		}
		if items, ok := p.propCompletion(path, src, off); ok {
			reply(map[string]any{"isIncomplete": false, "items": items})
			return true, ""
		}
	case "textDocument/definition":
		if loc, ok := p.routeTargetAt(path, src, off); ok {
			reply([]any{loc})
			return true, ""
		}
	case "textDocument/references":
		if routes := p.pageRoutesAt(path, src, off); len(routes) > 0 {
			go reply(p.pageReferences(m.Params, path, routes))
			return true, ""
		}
	}
	return false, ""
}

// sourceText is a source as the editor has it, else as it is on disk.
func (p *proxy) sourceText(path string) []byte {
	p.mu.Lock()
	b, ok := p.bufs[path]
	p.mu.Unlock()
	if ok {
		return b
	}
	b, _ = os.ReadFile(path)
	return b
}

// shapeHover is the note on a classified name, or the hover of an on…
// attribute, at off.
func shapeHover(path string, s *shapeScan, off int) any {
	if e, ok := s.eventAt(off); ok && off <= e.attr.end {
		return markdownHover(s.eventDoc(e), s.src, e.attr.start, e.attr.end)
	}
	if r, ok := s.refAt(off); ok {
		if doc := s.refDoc(path, r); doc != "" {
			return markdownHover(doc, s.src, r.start, r.end)
		}
	}
	return nil
}

// refDoc says what a name reads: its kind, its type and where it is set.
func (s *shapeScan) refDoc(path string, r shapeRef) string {
	line := func(off int) string { return strconv.Itoa(int(positionOf(s.src, off).Line) + 1) }
	switch r.kind {
	case refLocal:
		return "**local** `" + r.name + "` · declared by `" + r.local.stmt + "` (line " + line(r.local.off) + ")"
	case refEvent:
		doc := "**event** `" + r.name + "` · runs on the server · patches the page"
		if m := s.methods[r.name]; m != nil {
			doc += "\n\nThe method " + r.name + " of " + s.shape.Type + ", line " + line(m.off) + "."
		}
		return doc
	case refProp, refState, refBrowser:
	default:
		return ""
	}
	f := s.fields[r.name]
	if f == nil {
		return ""
	}
	doc := "**" + f.kind.String() + "** `" + f.name + " " + f.typ + "`"
	set := s.setters[f.name]
	switch f.kind {
	case refProp:
		attr := lowerCamel(f.name)
		if isPagePath(path) {
			doc += " · set from the URL: the `[" + attr + "]` path segment, else the `" + attr + "` query parameter"
		} else {
			doc += " · set by the `" + attr + "` attribute of a `<" + s.shape.Type + ">` tag"
		}
		if len(set) > 0 {
			doc += "; changed by " + s.setterList(set)
		}
	case refState:
		if len(set) > 0 {
			doc += " · set in " + s.setterList(set)
		} else {
			doc += " · set by no method of the header: its zero value, or injected when its type is provided"
		}
		if s.shape.Live {
			doc += "\n\nTracked: the server keeps it between events, and a change re-renders what reads it."
		}
	case refBrowser:
		doc += " · kept in the browser: a change re-renders what reads it, with no round trip"
		if len(set) > 0 {
			doc += "\n\nSet by " + strings.Join(set, ", ") + "."
		}
	}
	return doc
}

// setterList names the methods setting a field, each hook with its attribute.
func (s *shapeScan) setterList(set []string) string {
	var out []string
	for _, name := range set {
		if m := s.methods[name]; m != nil && m.hook != "" {
			out = append(out, "`"+name+"` (@ui."+m.hook+")")
		} else if m != nil {
			out = append(out, "`"+name+"`")
		} else {
			out = append(out, name)
		}
	}
	return strings.Join(out, ", ")
}

// eventDoc says where an on… attribute's handler runs: on the server (an
// event, which patches the page), in the browser (ui.JS commands, browser
// state assignments), or both in turn.
func (s *shapeScan) eventDoc(e eventAttr) string {
	value := ""
	if e.attr.valEnd > e.attr.valStart+1 {
		value = strings.TrimSpace(string(s.src[e.attr.valStart+1 : e.attr.valEnd-1]))
	}
	var events, states []string
	for _, r := range s.refs {
		if r.start < e.attr.valStart || r.end > e.attr.valEnd {
			continue
		}
		switch r.kind {
		case refEvent:
			events = appendNew(events, r.name)
		case refBrowser:
			states = appendNew(states, r.name)
		}
	}
	js := strings.Contains(value, "ui.JS") || strings.HasPrefix(value, "JS.")
	quoted := func(l []string) string { return "`" + strings.Join(l, "`, `") + "`" }
	head := "`" + e.attr.name + "` · "
	switch {
	case js && len(events) > 0:
		return head + "**browser commands**, then the event " + quoted(events) + " · run in the browser, then on the server · the event patches the page"
	case js:
		return head + "**browser commands** · run in the browser, with no round trip to the server"
	case len(states) > 0 && len(events) > 0:
		return head + "sets the browser state " + quoted(states) + ", then sends the event " + quoted(events) + " · runs in the browser, then on the server · the event patches the page"
	case len(states) > 0 && !strings.Contains(value, "("):
		return head + "**browser state** " + quoted(states) + " · set in the browser, with no round trip to the server"
	case len(events) > 0:
		return head + "**event** " + quoted(events) + " · runs on the server · patches the page"
	}
	return head + "**event** · runs on the server · patches the page"
}

func appendNew(l []string, s string) []string {
	for _, x := range l {
		if x == s {
			return l
		}
	}
	return append(l, s)
}

// lowerCamel is how a field is written as an attribute: title for Title,
// id for ID, urlPath for URLPath.
func lowerCamel(name string) string {
	n := 0
	for n < len(name) && name[n] >= 'A' && name[n] <= 'Z' {
		n++
	}
	switch {
	case n == 0:
		return name
	case n == len(name) || n == 1:
		return strings.ToLower(name[:n]) + name[n:]
	}
	return strings.ToLower(name[:n-1]) + name[n-1:]
}

// isPagePath reports whether a file is a page's: under a pages/ directory,
// in no private (_) folder, and no layout.
func isPagePath(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := len(parts) - 2; i >= 0; i-- {
		if parts[i] == "pages" {
			for _, p := range parts[i+1:] {
				if strings.HasPrefix(p, "_") {
					return false
				}
			}
			return true
		}
	}
	return false
}
