package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// lateCompletion answers a completion the main loop couldn't take in time —
// the first build of a large module holds the loop past goplsDeadline — when
// the spot needs no build: an attribute value on a component tag is answered
// from the sources alone (resolveComponent and the enum constants read
// files). Without it the editor gets an empty list and caches it for the
// typed prefix, so the first value completion after opening a file in a
// large module stays empty. nil when the spot isn't one of those.
func (p *proxy) lateCompletion(params json.RawMessage) any {
	var cp struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position lspPosition `json:"position"`
	}
	if json.Unmarshal(params, &cp) != nil {
		return nil
	}
	path := uriToPath(cp.TextDocument.URI)
	if !isVuka(path) {
		return nil
	}
	path = p.real(path)
	p.mu.Lock()
	src, open := p.bufs[path]
	modRoot := p.modRoot
	p.mu.Unlock()
	if modRoot == "" {
		return nil
	}
	if !open {
		// The didOpen is queued behind the build too; at first open the
		// buffer is the file.
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		src = b
	}
	off := offsetOf(src, cp.Position)
	if h := p.routeNameRequest("textDocument/completion", path, src, off); h != nil {
		// A route name: the names come from the folders until a build gives them.
		return h()
	}
	if !strings.HasSuffix(path, ".vuka.html") {
		return nil
	}
	spot, ok := completionSpot(src, off)
	if !ok || spot.kind != "value" || !isComponentTag(spot.tag.name) {
		return nil
	}
	vs := p.tagEnumValues(path, src, spot.tag.name, spot.attr)
	if len(vs) == 0 {
		return nil
	}
	typed := lspRange{positionOf(src, spot.from), cp.Position}
	items := make([]any, 0, len(vs))
	for i, v := range vs {
		label := v.value
		if label == "" {
			label = strconv.Quote(label)
		}
		item := map[string]any{
			"label": label, "kind": 20, "detail": or(v.doc, v.name),
			"sortText": fmt.Sprintf("%03d", i), "filterText": v.value,
			"labelDetails":     map[string]any{"description": v.name},
			"textEdit":         map[string]any{"range": typed, "newText": v.value},
			"insertTextFormat": 1,
		}
		items = append(items, item)
	}
	return map[string]any{"isIncomplete": false, "items": items}
}
