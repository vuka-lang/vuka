package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/format"
)

// rangeFormatting answers textDocument/rangeFormatting for a .vuka or
// .vuka.html file: the file formatted by vuka fmt's rules, and only the
// changes that touch the selected lines kept. A file that doesn't parse gets
// no edits.
func (p *proxy) rangeFormatting(m *rpcMsg, path string) {
	var params struct {
		Range lspRange `json:"range"`
	}
	_ = json.Unmarshal(m.Params, &params)
	p.mu.Lock()
	src, ok := p.bufs[path]
	p.mu.Unlock()
	if !ok {
		src, _ = os.ReadFile(path)
	}
	out, err := formatVuka(path, src)
	if err != nil {
		p.logf("format %s: %v", path, err)
		out = src
	}
	_ = p.editor.send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": rangeEdits(src, out, params.Range)})
}

// formatVuka is vuka fmt of a .vuka or .vuka.html file.
func formatVuka(path string, src []byte) ([]byte, error) {
	if strings.HasSuffix(path, ".vuka.html") {
		return format.Template(src, format.DirPackage(filepath.Dir(path)))
	}
	return format.Source(src)
}

// rangeEdits are the edits turning src into out that touch the lines of r.
func rangeEdits(src, out []byte, r lspRange) []any {
	edits := []any{}
	if bytes.Equal(src, out) {
		return edits
	}
	from := offsetOf(src, lspPosition{Line: r.Start.Line})
	to := offsetOf(src, lspPosition{Line: r.End.Line + 1})
	if r.End.Line > r.Start.Line && r.End.Character == 0 {
		to = offsetOf(src, lspPosition{Line: r.End.Line}) // a selection of whole lines
	}
	for _, h := range diffHunks(src, out) {
		overlap := h.from < to && h.fromEnd > from
		insert := h.from == h.fromEnd && h.from >= from && h.from < to
		if !overlap && !insert {
			continue
		}
		edits = append(edits, map[string]any{
			"range":   lspRange{positionOf(src, h.from), positionOf(src, h.fromEnd)},
			"newText": string(out[h.cur:h.curEnd]),
		})
	}
	return edits
}
