package main

import (
	"encoding/json"
	"strings"
)

// emptySemanticTokens is a well-formed empty semantic-tokens result.
func emptySemanticTokens() map[string]any { return map[string]any{"data": []uint32{}} }

// semToken is one decoded token in absolute coordinates.
type semToken struct{ line, char, length, typ, mods uint32 }

// semanticTokens remaps gopls's semantic tokens over a .vuka file's generated Go
// onto the source. gopls's data is delta-encoded quintuples [dLine, dChar,
// length, type, modifiers]; each is decoded to absolute generated coordinates,
// its place found in the source by the same map hover uses, tokens landing in
// generated-only or edited text dropped, and the survivors re-encoded as LSP
// deltas in source coordinates. The legend the editor saw is gopls's own with
// Vuka's markup types after it, so gopls's indices carry through unchanged; a
// .vuka.html file's markup tokens (lsp_semantic_html.go) are laid over them.
func (p *proxy) semanticTokens(vf *vfile, result json.RawMessage) map[string]any {
	if vf.isTempl() {
		return emptySemanticTokens()
	}
	var res struct {
		Data []uint32 `json:"data"`
	}
	if json.Unmarshal(result, &res) != nil || len(res.Data) < 5 {
		return p.markupOnlyTokens(vf)
	}
	toks := make([]semToken, 0, len(res.Data)/5)
	var line, char uint32
	for i := 0; i+5 <= len(res.Data); i += 5 {
		dLine, dChar, length := res.Data[i], res.Data[i+1], res.Data[i+2]
		if dLine == 0 {
			char += dChar
		} else {
			line += dLine
			char = dChar
		}
		if length == 0 {
			continue
		}
		if sl, sc, slen, ok := vf.tokenSource(line, char, length); ok {
			toks = append(toks, semToken{sl, sc, slen, res.Data[i+3], res.Data[i+4]})
		}
	}
	if strings.HasSuffix(vf.source, ".vuka.html") {
		// The markup's names, classified from the text, over gopls's.
		toks = mergeTokens(toks, p.markupTokens(vf.text()))
	}
	return encodeTokens(toks)
}

// tokenSource maps a token at (line, char) spanning length UTF-16 units in the
// generated Go to its line, char and length in the source. ok is false unless
// the whole token comes verbatim from one copied stretch and still exists in the
// buffer: a token in code Vuka wrote, or in a completable() placeholder, or in
// text edited away, has no faithful source range and is dropped.
func (f *vfile) tokenSource(line, char, length uint32) (uint32, uint32, uint32, bool) {
	if f.m == nil {
		return 0, 0, 0, false
	}
	start := offsetOf(f.gen, lspPosition{Line: line, Character: char})
	end := offsetOf(f.gen, lspPosition{Line: line, Character: char + length})
	if end <= start {
		return 0, 0, 0, false
	}
	s, sExact, sOK := f.m.ToSource(start)
	l, lExact, lOK := f.m.ToSource(end - 1)
	// The token must be one contiguous copied stretch: both ends map exactly and
	// the span keeps its length, so it is a verbatim identifier, not synthesized
	// Go nor a span crossing a source-map boundary.
	if !sOK || !sExact || !lOK || !lExact || l != s+(end-1-start) {
		return 0, 0, 0, false
	}
	if f.goneAt(s) || f.goneAt(l) {
		return 0, 0, 0, false
	}
	sp := positionOf(f.text(), f.curOff(s))
	ep := positionOf(f.text(), f.curOff(l+1))
	if ep.Line != sp.Line || ep.Character <= sp.Character {
		return 0, 0, 0, false
	}
	return sp.Line, sp.Character, ep.Character - sp.Character, true
}
