package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

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

// TestMentionsVirtual: a message names a generated file by its URI, alone
// or followed by a position, and an owned one is the editor's.
func TestMentionsVirtual(t *testing.T) {
	gen := filepath.FromSlash("/m/pages/[id]/page_vuka.go")
	owned := filepath.FromSlash("/m/x_templ.go")
	p := &proxy{virtual: map[string]*vfile{gen: {}, owned: {}}, owned: map[string]bool{owned: true}}
	for raw, want := range map[string]bool{
		`{"uri":"` + pathToURI(gen) + `"}`:                           true,
		`{"message":"see ` + pathToURI(gen) + `:12:3 for it"}`:       true,
		`{"uri":"` + pathToURI(gen) + `#L12"}`:                       true,
		`{"uri":"` + pathToURI(owned) + `"}`:                         false,
		`{"uri":"` + pathToURI(filepath.FromSlash("/m/a.go")) + `"}`: false,
		`{"uri":"file:///m/pages/page_vuka.go"}`:                     false,
		`[]`:                                                         false,
	} {
		if got := p.mentionsVirtual(json.RawMessage(raw)); got != want {
			t.Errorf("%s: %v, want %v", raw, got, want)
		}
	}
	p.mu.Lock()
	p.virtual, p.virtualURIs = map[string]*vfile{}, nil
	p.mu.Unlock()
	if p.mentionsVirtual(json.RawMessage(`{"uri":"` + pathToURI(gen) + `"}`)) {
		t.Error("a file gone from virtual is still named")
	}
}

// TestLSPPublishOnce: a build that leaves a file's diagnostics as they were
// doesn't publish them again: neither a file the editor hasn't opened nor
// one it has open and doesn't edit. The edited file hears back once an
// edit at least.
func TestLSPPublishOnce(t *testing.T) {
	broken := strings.Replace(matchSource, "\tcase Err(e):\n\t\treturn nil, \"\", e\n", "", 1)
	other := "package main\n\nfunc helper() int { return 1 }\n"
	third := "package lib\n\n// Third is three.\nfunc Third() int { return 3 }\n"
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": broken, "other.vuka": other, "lib/lib.vuka": third})
	mainURI := pathToURI(filepath.Join(dir, "main.vuka"))
	otherURI := pathToURI(filepath.Join(dir, "other.vuka"))
	thirdURI := pathToURI(filepath.Join(dir, "lib", "lib.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": otherURI, "languageId": "vuka", "version": 1, "text": other}})
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": thirdURI, "languageId": "vuka", "version": 1, "text": third}})
	c.waitDiags(mainURI, func(ds []any) bool { return strings.Contains(jsonOf(ds), `"code":"E0113"`) })
	builds := func() int { return strings.Count(c.log.String(), "built the module") }
	// settle waits for the next build of the module, and counts the
	// publications that follow it for each file.
	settle := func() map[any]int {
		t.Helper()
		n := builds()
		for deadline := time.Now().Add(20 * time.Second); builds() == n && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		}
		time.Sleep(time.Second)
		count := map[any]int{}
		for {
			select {
			case p := <-c.diags:
				count[p["uri"]]++
				continue
			default:
			}
			return count
		}
	}
	settle()
	for i := 2; i <= 4; i++ {
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": otherURI, "version": i},
			"contentChanges": []any{map[string]any{"text": other + fmt.Sprintf("\n// edit %d\n", i)}}})
		n := settle()
		if n[mainURI] > 0 || n[thirdURI] > 0 {
			t.Fatalf("edit %d: published again: %v", i, n)
		}
		if n[otherURI] == 0 {
			t.Fatalf("edit %d: the edited file heard nothing", i)
		}
	}
	// Closed and opened again, a file is told its diagnostics.
	c.notify("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": thirdURI}})
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": thirdURI, "languageId": "vuka", "version": 2, "text": third}})
	c.waitDiags(thirdURI, func([]any) bool { return true })
}

// TestTSSyncLatest: edits made faster than the TypeScript server is synced
// reach it in order, ending with the editor's last text, and coalesce.
func TestTSSyncLatest(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.vuka.html")
	p := &proxy{root: root, bufs: map[string][]byte{}}
	out := &lockedBuffer{}
	ts := &tsServer{p: p, conn: newRPCConn(strings.NewReader(""), out), docs: map[string]*tsDoc{}, typesAt: map[string]time.Time{}, locs: map[string][]dtsLoc{}}
	text := func(i int) []byte {
		return []byte(fmt.Sprintf("package main\n\n<div></div>\n\n<script lang=\"ts\">\nconst n = %d\n</script>\n", i))
	}
	const edits = 300
	for i := 0; i < edits; i++ {
		p.mu.Lock()
		p.bufs[path] = text(i)
		p.mu.Unlock()
		ts.syncLatest(path)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		ts.mu.Lock()
		_, running := ts.syncs[path]
		ts.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the worker didn't finish")
		}
	}
	in := newRPCConn(strings.NewReader(out.String()), nil)
	version, last, n := 0, "", 0
	for {
		body, err := in.read()
		if err != nil {
			break
		}
		var m struct {
			Params struct {
				TextDocument struct {
					Version int    `json:"version"`
					Text    string `json:"text"`
				} `json:"textDocument"`
				ContentChanges []struct {
					Text string `json:"text"`
				} `json:"contentChanges"`
			} `json:"params"`
		}
		_ = json.Unmarshal(body, &m)
		v, txt := m.Params.TextDocument.Version, m.Params.TextDocument.Text
		if len(m.Params.ContentChanges) > 0 {
			txt = m.Params.ContentChanges[0].Text
		}
		if v <= version {
			t.Fatalf("version %d after %d", v, version)
		}
		version, last, n = v, txt, n+1
	}
	if want := fmt.Sprintf("const n = %d\n", edits-1); !strings.Contains(last, want) {
		t.Fatalf("the server's last text has no %q: %q", want, last)
	}
	if n >= edits {
		t.Errorf("%d edits sent %d times: none coalesced", edits, n)
	}
	t.Logf("%d edits, %d messages", edits, n)
}
