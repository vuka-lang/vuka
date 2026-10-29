package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// bigSource is a .vuka file of about 2000 lines and 114KB, a plain Go
// package, whose generated Go copies nearly all of it.
func bigSource() string {
	var b strings.Builder
	b.WriteString("package main\n\nimport \"fmt\"\n\n")
	for i := 0; b.Len() < 114<<10; i++ {
		fmt.Fprintf(&b, `// Describe%[1]d describes record number %[1]d in a sentence a person reads.
func Describe%[1]d(id int, name string, values []int) string {
	total := 0
	for _, value := range values {
		total += value * id
	}
	label := fmt.Sprintf("record %%d named %%s has total %%d", id, name, total)
	return label
}

`, i)
	}
	return b.String()
}

var benchIdent = regexp.MustCompile(`[A-Za-z_]\w*`)

// genTokens is gopls's semantic tokens, delta-encoded, for up to n
// identifiers of vf's generated Go.
func genTokens(vf *vfile, n int) json.RawMessage {
	var toks []semToken
	for _, loc := range benchIdent.FindAllIndex(vf.gen, n) {
		p := positionOf(vf.gen, loc[0])
		toks = append(toks, semToken{p.Line, p.Character, uint32(loc[1] - loc[0]), 1, 0})
	}
	b, _ := json.Marshal(map[string]any{"data": encodeAbs(toks...)})
	return b
}

func bigVFile(tb testing.TB) *vfile {
	src := bigSource()
	res, err := transpile.Package([]transpile.File{{Name: "main.vuka", Src: []byte(src)}}, transpile.Options{})
	if err != nil {
		tb.Fatal(err)
	}
	out := res.Files[0]
	vf := &vfile{source: "/p/main.vuka", from: []byte(src), gen: out.Src, m: out.Map, version: 1}
	vf.index()
	return vf
}

// BenchmarkSemanticTokens maps 10,000 of gopls's tokens on a 2000-line,
// 114KB file onto the source.
func BenchmarkSemanticTokens(b *testing.B) {
	vf := bigVFile(b)
	data := genTokens(vf, 10000)
	p := &proxy{}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.semanticTokens(vf, data)
	}
}

// BenchmarkMentionsVirtual checks a 64KB message of gopls's, naming no
// generated file, against 1000 generated files.
func BenchmarkMentionsVirtual(b *testing.B) {
	p := &proxy{virtual: map[string]*vfile{}, owned: map[string]bool{}}
	for i := 0; i < 1000; i++ {
		p.virtual[filepath.FromSlash(fmt.Sprintf("/m/p%03d/page_vuka.go", i))] = &vfile{}
	}
	var items []string
	for i := 0; len(strings.Join(items, ",")) < 64<<10; i++ {
		items = append(items, fmt.Sprintf(`{"uri":%q,"range":{"start":{"line":%d,"character":1},"end":{"line":%d,"character":9}}}`,
			pathToURI(filepath.FromSlash(fmt.Sprintf("/m/lib/f%d.go", i%40))), i, i))
	}
	raw := json.RawMessage("[" + strings.Join(items, ",") + "]")
	if p.mentionsVirtual(raw) {
		b.Fatal("mentions a generated file")
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.mentionsVirtual(raw)
	}
}

// TestLineIndex: the line index maps positions and offsets as offsetOf
// and positionOf do, in UTF-16 columns, past the end of a line or of the
// buffer, and on an empty buffer or one ending in a newline.
func TestLineIndex(t *testing.T) {
	for _, buf := range []string{"", "\n", "a", "a\n", "héllo 😀 wörld\n\tx := \"𝄞\"\r\n\nlast", "😀😀\n😀"} {
		x := newLineIndex([]byte(buf))
		for off := -1; off <= len(buf)+1; off++ {
			if got, want := x.position(off), positionOf([]byte(buf), off); got != want {
				t.Errorf("%q: position(%d) = %v, want %v", buf, off, got, want)
			}
		}
		for line := uint32(0); line < 5; line++ {
			for char := uint32(0); char < 20; char++ {
				pos := lspPosition{line, char}
				if got, want := x.offset(pos), offsetOf([]byte(buf), pos); got != want {
					t.Errorf("%q: offset(%v) = %d, want %d", buf, pos, got, want)
				}
			}
		}
	}
}

// TestIndexedVFile: an indexed vfile, edited, maps as one that isn't.
func TestIndexedVFile(t *testing.T) {
	plain := bigVFile(t)
	plain.genLines, plain.fromLines = nil, nil
	indexed := bigVFile(t)
	cur := []byte(strings.Replace(string(plain.from), "total := 0", "total := 0 // 😀 édité", 3))
	plain.withText(cur, nil)
	indexed.withText(cur, nil)
	if indexed.curLines == nil || plain.curLines != nil {
		t.Fatal("withText indexes the text of an indexed vfile only")
	}
	data := genTokens(plain, 3000)
	p := &proxy{}
	if a, b := jsonOf(p.semanticTokens(plain, data)), jsonOf(p.semanticTokens(indexed, data)); a != b {
		t.Fatal("indexed tokens differ")
	}
	for off := 0; off < len(plain.gen); off += 97 {
		pos := positionOf(plain.gen, off)
		r := lspRange{pos, positionOf(plain.gen, off+5)}
		a, aok := plain.toSource(r, false)
		b, bok := indexed.toSource(r, false)
		if a != b || aok != bok {
			t.Fatalf("toSource(%v): %v %v, indexed %v %v", r, a, aok, b, bok)
		}
	}
	for off := 0; off < len(cur); off += 89 {
		pos := positionOf(cur, off)
		if a, b := plain.toGen(pos), indexed.toGen(pos); a != b {
			t.Fatalf("toGen(%v): %v, indexed %v", pos, a, b)
		}
	}
}
