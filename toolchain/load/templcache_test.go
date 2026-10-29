package load

import (
	"bytes"
	"testing"

	"github.com/vuka-lang/vuka/v2/transpile"
)

var templCalls, instrumentCalls int

func countingTempl(path, name string, src []byte, scopes map[string]string) ([]byte, any, *transpile.Error) {
	templCalls++
	if bytes.Contains(src, []byte("broken")) {
		return nil, nil, &transpile.Error{Msg: "broken", Code: "E0006"}
	}
	out := append([]byte("package p\n// "), src...)
	for k, v := range scopes {
		out = append(out, " "+k+"="+v...)
	}
	return out, nil, nil
}

func countingInstrument(src []byte) ([]byte, error) {
	instrumentCalls++
	return append([]byte("// live\n"), src...), nil
}

func otherTempl(path, name string, src []byte, scopes map[string]string) ([]byte, any, *transpile.Error) {
	templCalls++
	return []byte("package p // other\n"), nil, nil
}

// TestCompileTemplCache: a .templ file is compiled again only when its
// text, its options, or the compiler or instrumenter changed; what a
// caller gets shares nothing it may change with the cache.
func TestCompileTemplCache(t *testing.T) {
	oldC, oldI := TemplCompiler, TemplInstrument
	t.Cleanup(func() {
		TemplCompiler, TemplInstrument = oldC, oldI
		templs.Lock()
		templs.files = nil
		templs.Unlock()
	})
	TemplCompiler, TemplInstrument = countingTempl, countingInstrument
	templs.Lock()
	templs.files = nil
	templs.Unlock()
	templCalls, instrumentCalls = 0, 0
	compile := func(src string, live bool, scopes map[string]string) *Templ {
		return compileTempl("/m/page.templ", "page.templ", []byte(src), live, scopes)
	}
	want := func(what string, calls, instruments int) {
		t.Helper()
		if templCalls != calls || instrumentCalls != instruments {
			t.Fatalf("%s: compiled %d times, instrumented %d; want %d, %d", what, templCalls, instrumentCalls, calls, instruments)
		}
	}
	a := compile("templ A() {}", true, nil)
	want("first", 1, 1)
	b := compile("templ A() {}", true, nil)
	want("same text", 1, 1)
	if !bytes.Equal(a.Go, b.Go) || !bytes.Equal(a.Live, b.Live) || b.GoName != "page_templ.go" {
		t.Fatalf("a hit differs: %q %q", a.Go, b.Go)
	}
	b.Go[0], b.Live[0] = 'X', 'X'
	if c := compile("templ A() {}", true, nil); c.Go[0] != 'p' || c.Live[0] != '/' {
		t.Fatal("a caller's change reached the cache")
	}
	want("same text again", 1, 1)
	compile("templ A() { x }", true, nil)
	want("new text", 2, 2)
	compile("templ A() { x }", false, nil)
	want("not live", 3, 2)
	compile("templ A() { x }", false, map[string]string{"A": "s1"})
	want("scoped", 4, 2)
	compile("templ A() { x }", false, map[string]string{"A": "s1"})
	want("scoped again", 4, 2)
	compile("templ A() { x }", false, map[string]string{"A": "s2"})
	want("another scope", 5, 2)
	TemplCompiler = otherTempl
	if c := compile("templ A() { x }", false, map[string]string{"A": "s2"}); string(c.Go) != "package p // other\n" {
		t.Fatalf("another compiler's Go: %q", c.Go)
	}
	want("another compiler", 6, 2)
	TemplCompiler = countingTempl
	e1 := compile("broken", true, nil)
	e2 := compile("broken", true, nil)
	want("an error", 7, 2)
	if e1.Err == nil || e2.Err == nil || e1.Err == e2.Err {
		t.Fatal("a cached error is the caller's own")
	}
}
