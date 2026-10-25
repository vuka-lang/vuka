package main

import (
	"bytes"
	"compress/gzip"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

func unbrotli(t *testing.T, b []byte) []byte {
	t.Helper()
	out, err := io.ReadAll(brotli.NewReader(bytes.NewReader(b)))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func gunzip(t *testing.T, b []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

var bigJS = []byte(strings.Repeat("export function pet(i) { return { id: i, name: 'Rex', status: 'available' } }\n", 60))

func TestPrecompress(t *testing.T) {
	e := precompress("app.js", bigJS)
	if e.br == nil || e.gz == nil || len(e.br) >= len(e.gz) {
		t.Fatalf("encodings: br %d, gzip %d of %d", len(e.br), len(e.gz), len(bigJS))
	}
	if !bytes.Equal(unbrotli(t, e.br), bigJS) || !bytes.Equal(gunzip(t, e.gz), bigJS) {
		t.Error("an encoding differs from its source")
	}
	for name, data := range map[string][]byte{"small.js": []byte("export {}"), "photo.png": bytes.Repeat([]byte{0}, 4096), "font.woff2": bytes.Repeat([]byte{0}, 4096)} {
		if e := precompress(name, data); e.br != nil || e.gz != nil {
			t.Errorf("%s was precompressed", name)
		}
	}
}

// The encodings vuka build embeds decode to the assets they stand for.
func TestAssetsGoEncodings(t *testing.T) {
	island := []byte(strings.Repeat("export default function mount(el) { el.textContent = 'pets' }\n", 40))
	c := &compiled{
		manifest: []byte(`{}`),
		bundles:  []*bundle{{name: "counter.abc123.js", js: bigJS}, {name: "tiny.def456.js", js: []byte("export {}")}},
		islands:  []*islandBundle{{name: "chart.0a1b2c.js", js: island}},
	}
	src := assetsGo(c, true)
	consts, calls := generatedConsts(t, src)
	want := map[string][]byte{"/_vuka/c/counter.abc123.js": bigJS, "/_vuka/i/chart.0a1b2c.js": island}
	if len(calls) != 4 {
		t.Fatalf("RegisterEncoded calls: %q", calls)
	}
	for _, call := range calls {
		data := want[call[0]]
		if data == nil {
			t.Errorf("an encoding of %s", call[0])
			continue
		}
		got := consts[call[2]]
		if call[1] == "br" {
			got = unbrotli(t, got)
		} else {
			got = gunzip(t, got)
		}
		if !bytes.Equal(got, data) {
			t.Errorf("the %s of %s differs from its source", call[1], call[0])
		}
	}
	if plain := assetsGo(c, false); bytes.Contains(plain, []byte("RegisterEncoded")) {
		t.Error("an old web gets RegisterEncoded calls")
	}
}

var encodedCall = regexp.MustCompile(`vukaweb\.RegisterEncoded\("([^"]+)", "([^"]+)", \[\]byte\((\w+)\)\)`)

// generatedConsts is a generated file's string constants, and its
// RegisterEncoded calls as path, encoding and constant.
func generatedConsts(t *testing.T, src []byte) (map[string][]byte, [][3]string) {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "gen.go", src, 0)
	if err != nil {
		t.Fatalf("the generated file doesn't parse: %v", err)
	}
	consts := map[string][]byte{}
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, s := range g.Specs {
			vs := s.(*ast.ValueSpec)
			v, err := strconv.Unquote(vs.Values[0].(*ast.BasicLit).Value)
			if err != nil {
				t.Fatal(err)
			}
			consts[vs.Names[0].Name] = []byte(v)
		}
	}
	var calls [][3]string
	for _, m := range encodedCall.FindAllStringSubmatch(string(src), -1) {
		calls = append(calls, [3]string{m[1], m[2], m[3]})
	}
	return consts, calls
}

func TestWriteEncoded(t *testing.T) {
	file := filepath.Join(t.TempDir(), "chart.0a1b2c.js")
	os.WriteFile(file, bigJS, 0o644)
	names, err := writeEncoded(file, bigJS)
	if err != nil || len(names) != 2 || names[0] != "chart.0a1b2c.js.br" || names[1] != "chart.0a1b2c.js.gz" {
		t.Fatalf("written: %v %v", names, err)
	}
	br, _ := os.ReadFile(file + ".br")
	gz, _ := os.ReadFile(file + ".gz")
	if !bytes.Equal(unbrotli(t, br), bigJS) || !bytes.Equal(gunzip(t, gz), bigJS) {
		t.Error("the files beside it differ from the source")
	}
	// A file grown too small for them loses them.
	if names, _ := writeEncoded(file, []byte("export {}")); len(names) != 0 {
		t.Errorf("a small file's encodings: %v", names)
	}
	if _, err := os.Stat(file + ".br"); err == nil {
		t.Error("a stale .br stayed")
	}
}
