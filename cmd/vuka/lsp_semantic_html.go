package main

import (
	"sort"
	"strings"
	"sync"
)

// Semantic tokens for shape markup: the names scanShape classifies, as
// token types of Vuka's own appended to gopls's legend, so a theme colours a
// prop, server state, browser state, an event, a component and a template
// local apart. The VS Code extension declares the types (semanticTokenTypes)
// and the TextMate scopes they fall back to (semanticTokenScopes).

// vukaTokenTypes are the token types vuka lsp adds after gopls's, in refKind
// order from refProp.
// vukaContextVar, last, is a context processor's variable read by name.
var vukaTokenTypes = []string{"vukaProp", "vukaState", "vukaBrowserState", "vukaEvent", "vukaComponent", "vukaLocal", "vukaContextVar"}

// semState is what the proxy keeps for markup tokens, code lenses and route
// links.
type semState struct {
	mu      sync.Mutex
	base    uint32 // the legend's index of vukaTokenTypes[0]
	decl    uint32 // the declaration modifier's bit, 0 when the legend has none
	routes  []*folderRoute
	modRoot string
}

// extendCaps adds, to initialize's capabilities, the markup token types, code
// lenses on pages, and the renames of files under pages/ that vuka lsp
// answers itself.
func (p *proxy) extendCaps(caps map[string]any) {
	stp, _ := caps["semanticTokensProvider"].(map[string]any)
	if stp == nil {
		stp = map[string]any{"legend": map[string]any{"tokenTypes": []any{}, "tokenModifiers": []any{"declaration"}}, "full": true, "range": true}
		caps["semanticTokensProvider"] = stp
	}
	legend, _ := stp["legend"].(map[string]any)
	if legend == nil {
		legend = map[string]any{"tokenTypes": []any{}, "tokenModifiers": []any{}}
		stp["legend"] = legend
	}
	types, _ := legend["tokenTypes"].([]any)
	mods, _ := legend["tokenModifiers"].([]any)
	p.sem.mu.Lock()
	p.sem.base = uint32(len(types))
	for i, m := range mods {
		if m == "declaration" && i < 32 {
			p.sem.decl = 1 << i
		}
	}
	p.sem.mu.Unlock()
	for _, t := range vukaTokenTypes {
		types = append(types, t)
	}
	legend["tokenTypes"] = types
	if caps["codeLensProvider"] == nil {
		caps["codeLensProvider"] = map[string]any{}
	}
	ws, _ := caps["workspace"].(map[string]any)
	if ws == nil {
		ws = map[string]any{}
		caps["workspace"] = ws
	}
	ops, _ := ws["fileOperations"].(map[string]any)
	if ops == nil {
		ops = map[string]any{}
		ws["fileOperations"] = ops
	}
	ops["willRename"] = map[string]any{"filters": []any{
		map[string]any{"scheme": "file", "pattern": map[string]any{"glob": "**/pages/**", "matches": "file"}},
		map[string]any{"scheme": "file", "pattern": map[string]any{"glob": "**/pages/**", "matches": "folder"}},
	}}
}

// markupTokens are the tokens of a .vuka.html file's classified names.
func (p *proxy) markupTokens(src []byte) []semToken {
	p.sem.mu.Lock()
	base, decl := p.sem.base, p.sem.decl
	p.sem.mu.Unlock()
	return shapeTokens(scanShape(src), base, decl)
}

func shapeTokens(s *shapeScan, base, decl uint32) []semToken {
	var out []semToken
	for _, r := range s.refs {
		if r.kind == refNone || r.end <= r.start {
			continue
		}
		start, end := positionOf(s.src, r.start), positionOf(s.src, r.end)
		if start.Line != end.Line {
			continue
		}
		var mods uint32
		if r.decl {
			mods = decl
		}
		out = append(out, semToken{start.Line, start.Character, end.Character - start.Character, base + uint32(r.kind-refProp), mods})
	}
	return out
}

// mergeTokens lays the markup's tokens over gopls's: a gopls token
// overlapping one of them is dropped.
func mergeTokens(gopls, markup []semToken) []semToken {
	if len(markup) == 0 {
		return gopls
	}
	type span struct{ line, from, to uint32 }
	taken := map[uint32][]span{}
	for _, t := range markup {
		taken[t.line] = append(taken[t.line], span{t.line, t.char, t.char + t.length})
	}
	out := append([]semToken{}, markup...)
	for _, t := range gopls {
		clash := false
		for _, s := range taken[t.line] {
			if t.char < s.to && t.char+t.length > s.from {
				clash = true
				break
			}
		}
		if !clash {
			out = append(out, t)
		}
	}
	return out
}

// encodeTokens sorts tokens and delta-encodes them as LSP sends them.
func encodeTokens(toks []semToken) map[string]any {
	sort.Slice(toks, func(i, j int) bool {
		if toks[i].line != toks[j].line {
			return toks[i].line < toks[j].line
		}
		return toks[i].char < toks[j].char
	})
	data := make([]uint32, 0, len(toks)*5)
	var pl, pc uint32
	for _, t := range toks {
		dLine := t.line - pl
		dChar := t.char
		if dLine == 0 {
			dChar = t.char - pc
		}
		data = append(data, dLine, dChar, t.length, t.typ, t.mods)
		pl, pc = t.line, t.char
	}
	return map[string]any{"data": data}
}

// markupOnlyTokens are a file's tokens when gopls has none for it: the
// markup's for a .vuka.html file, none for others.
func (p *proxy) markupOnlyTokens(vf *vfile) map[string]any {
	if vf == nil || !strings.HasSuffix(vf.source, ".vuka.html") {
		return emptySemanticTokens()
	}
	return encodeTokens(p.markupTokens(vf.text()))
}
