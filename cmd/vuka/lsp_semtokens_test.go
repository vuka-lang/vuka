package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// genVFile transpiles src and returns a settled vfile for it, as the proxy holds
// after a build.
func genVFile(t *testing.T, src string) *vfile {
	t.Helper()
	res, err := transpile.Package([]transpile.File{{Name: "main.vuka", Src: []byte(src)}}, transpile.Options{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	out := res.Files[0]
	return &vfile{source: "/p/main.vuka", from: []byte(src), gen: out.Src, m: out.Map, version: 1}
}

// absToken is an absolute token over the nth occurrence of ident in the
// generated Go, with the given token type.
func absToken(t *testing.T, vf *vfile, ident string, nth, typ uint32) semToken {
	t.Helper()
	off := -1
	for i := 0; nth > 0; nth-- {
		j := strings.Index(string(vf.gen[i:]), ident)
		if j < 0 {
			t.Fatalf("%q not found in generated Go", ident)
		}
		off = i + j
		i = off + len(ident)
	}
	pos := positionOf(vf.gen, off)
	return semToken{pos.Line, pos.Character, uint32(len(ident)), typ, 0}
}

// encodeAbs delta-encodes absolute tokens as gopls sends them.
func encodeAbs(toks ...semToken) []uint32 {
	var data []uint32
	var pl, pc uint32
	for _, t := range toks {
		dl := t.line - pl
		dc := t.char
		if dl == 0 {
			dc = t.char - pc
		}
		data = append(data, dl, dc, t.length, t.typ, t.mods)
		pl, pc = t.line, t.char
	}
	return data
}

// genTokenData is the single-token gopls data stream for absToken.
func genTokenData(t *testing.T, vf *vfile, ident string, nth, typ uint32) []uint32 {
	t.Helper()
	return encodeAbs(absToken(t, vf, ident, nth, typ))
}

// decodeTokens turns a {data} result back into absolute tokens.
func decodeTokens(t *testing.T, result map[string]any) []semToken {
	t.Helper()
	b, _ := json.Marshal(result["data"])
	var data []uint32
	_ = json.Unmarshal(b, &data)
	var out []semToken
	var line, char uint32
	for i := 0; i+5 <= len(data); i += 5 {
		if data[i] == 0 {
			char += data[i+1]
		} else {
			line += data[i]
			char = data[i+1]
		}
		out = append(out, semToken{line, char, data[i+2], data[i+3], data[i+4]})
	}
	return out
}

const semTokSrc = `package p

type Widget struct{ Qty int }

func use() {
	var w Widget
	_ = w
}
`

// TestSemanticTokensTransform (unit): a token over a verbatim type reference in
// the generated Go maps back to the same identifier in the .vuka source, and a
// token in the generated-only header is dropped.
func TestSemanticTokensTransform(t *testing.T) {
	p := &proxy{}
	vf := genVFile(t, semTokSrc)

	// The type use `Widget` inside use() is the 2nd occurrence (1st is the
	// declaration). It is copied verbatim, so it must map to the source.
	data := genTokenData(t, vf, "Widget", 2, 7 /* any type index */)
	got := decodeTokens(t, p.semanticTokens(vf, mustJSON(t, map[string]any{"data": data})))
	if len(got) != 1 {
		t.Fatalf("want 1 mapped token, got %d: %+v", len(got), got)
	}
	wantOff := strings.Index(semTokSrc, "var w Widget") + len("var w ")
	want := positionOf([]byte(semTokSrc), wantOff)
	if got[0].line != want.Line || got[0].char != want.Character || got[0].length != 6 || got[0].typ != 7 {
		t.Fatalf("mapped token %+v, want line %d char %d len 6 type 7", got[0], want.Line, want.Character)
	}

	// A token in the generated header (`Code` in the DO-NOT-EDIT banner) is
	// generated-only and must be dropped.
	hdr := genTokenData(t, vf, "Code", 1, 1)
	if got := decodeTokens(t, p.semanticTokens(vf, mustJSON(t, map[string]any{"data": hdr}))); len(got) != 0 {
		t.Fatalf("generated-only token should be dropped, got %+v", got)
	}

	// Two verbatim tokens (the type Widget and its field N on the decl line)
	// re-encode as sorted source-coordinate deltas.
	twoData := encodeAbs(absToken(t, vf, "Widget", 1, 7), absToken(t, vf, "Qty", 1, 8))
	two := decodeTokens(t, p.semanticTokens(vf, mustJSON(t, map[string]any{"data": twoData})))
	if len(two) != 2 || two[0].line > two[1].line || (two[0].line == two[1].line && two[0].char >= two[1].char) {
		t.Fatalf("expected 2 sorted tokens, got %+v", two)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

const semTokMain = `package main

type Widget struct{ Qty int }

func mk[T any]() T { var z T; return z }

func use() {
	w := mk[Widget]()
	_ = w.Qty
}
`

// TestLSPSemanticTokensEndToEnd drives the real proxy+gopls: the legend is
// advertised, a semanticTokens/full request for a .vuka file comes back with the
// type reference Widget (used as a type argument and as a selector base) tokened
// at the right source positions, with no token in generated-only space.
func TestLSPSemanticTokensEndToEnd(t *testing.T) {
	c, dir, init := startLSPWith(t, map[string]string{"main.vuka": semTokMain})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))

	// The server advertises a semanticTokensProvider with gopls's legend.
	legend := semTokLegendFrom(t, init)
	typeIdx, ok := legend["type"]
	if !ok {
		t.Skip("gopls legend has no 'type' token; skipping")
	}

	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": "vuka", "version": 1, "text": semTokMain}})
	c.waitDiags(uri, func(ds []any) bool { return true })

	v := c.call("textDocument/semanticTokens/full", map[string]any{"textDocument": map[string]any{"uri": uri}})
	res, _ := v.(map[string]any)
	if res == nil {
		t.Fatalf("no semanticTokens result: %v", v)
	}
	toks := decodeTokens(t, res)
	if len(toks) == 0 {
		t.Fatalf("no semantic tokens returned")
	}
	// Widget as a type argument: mk[Widget]().
	argOff := strings.Index(semTokMain, "mk[Widget]") + len("mk[")
	argPos := positionOf([]byte(semTokMain), argOff)
	// Widget in its declaration: type Widget struct.
	declOff := strings.Index(semTokMain, "type Widget") + len("type ")
	declPos := positionOf([]byte(semTokMain), declOff)

	foundArg, foundType := false, false
	lines := uint32(strings.Count(semTokMain, "\n"))
	for _, tk := range toks {
		if tk.line > lines {
			t.Fatalf("token on line %d is past the source (generated-only leaked): %+v", tk.line, tk)
		}
		if tk.line == argPos.Line && tk.char == argPos.Character && tk.length == 6 {
			foundArg = true
			if tk.typ == typeIdx {
				foundType = true
			}
		}
		if tk.line == declPos.Line && tk.char == declPos.Character {
			foundType = foundType || tk.typ == typeIdx
		}
	}
	if !foundArg {
		t.Fatalf("no token on the Widget type argument at %d:%d; tokens=%+v", argPos.Line, argPos.Character, toks)
	}
	if !foundType {
		t.Fatalf("Widget was not tokened as a type (legend index %d); tokens=%+v", typeIdx, toks)
	}
}

// semTokLegendFrom reads the token-type → index map the server advertised at
// initialize.
func semTokLegendFrom(t *testing.T, init any) map[string]uint32 {
	t.Helper()
	b, _ := json.Marshal(init)
	var r struct {
		Capabilities struct {
			SemanticTokensProvider struct {
				Legend struct {
					TokenTypes []string `json:"tokenTypes"`
				} `json:"legend"`
			} `json:"semanticTokensProvider"`
		} `json:"capabilities"`
	}
	if json.Unmarshal(b, &r) != nil {
		t.Fatal("no initialize result")
	}
	types := r.Capabilities.SemanticTokensProvider.Legend.TokenTypes
	if len(types) == 0 {
		t.Skip("server did not advertise a semanticTokens legend")
	}
	m := map[string]uint32{}
	for i, name := range types {
		m[name] = uint32(i)
	}
	return m
}
