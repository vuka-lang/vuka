package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// scanModule writes latencyModule(n) with go.mod to a directory, the
// first package's file open in the editor.
func scanModule(tb testing.TB, n int) (dir string, bufs map[string][]byte, cursors map[string]int) {
	dir = tb.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module lsptest\n\ngo 1.23\n"), 0o644)
	bufs, cursors = map[string][]byte{}, map[string]int{}
	for name, src := range latencyModule(n) {
		path := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte(src), 0o644)
		if name == "p00/p00.vuka" {
			bufs[path], cursors[path] = []byte(src+"\nfunc typing() { x := core.\n}\n"), len(src)+26
		}
	}
	return dir, bufs, cursors
}

// BenchmarkBuildScans is a build's reading of the module before it
// transpiles: discovery, folder routes and decorators, over 48 packages.
func BenchmarkBuildScans(b *testing.B) {
	dir, bufs, cursors := scanModule(b, 48)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		read := buildReader(bufs, cursors, map[string][]hunk{})
		if _, err := load.Discover(dir, "lsptest", dir, true, read); err != nil {
			b.Fatal(err)
		}
		scanRoutes(dir, "lsptest", read)
		findDecorators(dir, "lsptest", read)
	}
}

// TestDecoratorScanCache: a file's decorators are read again only when its
// text changed, and a file's change shows in the next scan.
func TestDecoratorScanCache(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module lsptest\n\ngo 1.23\n"), 0o644)
	path := filepath.Join(dir, "deco", "deco.vuka")
	os.MkdirAll(filepath.Dir(path), 0o755)
	src := "package deco\n\ndecorator Logged(c) {\n}\n"
	os.WriteFile(path, []byte(src), 0o644)
	read := buildReader(map[string][]byte{}, map[string]int{}, map[string][]hunk{})
	names := func(ds []projectDecorator) (out []string) {
		for _, d := range ds {
			out = append(out, d.importPath+"."+d.name)
		}
		return out
	}
	scanned := func() *projectDecorator {
		decoScans.Lock()
		defer decoScans.Unlock()
		return &decoScans.files[path].decos[0]
	}
	if got := names(findDecorators(dir, "lsptest", read)); len(got) != 1 || got[0] != "lsptest/deco.Logged" {
		t.Fatalf("found %v", got)
	}
	first := scanned()
	findDecorators(dir, "lsptest", read)
	if scanned() != first {
		t.Fatal("an unchanged file was read again")
	}
	// The editor's buffer is what is read.
	bufs := map[string][]byte{path: []byte(src + "\ndecorator Timed(c) {\n}\n")}
	read = buildReader(bufs, map[string]int{path: -1}, map[string][]hunk{})
	if got := names(findDecorators(dir, "lsptest", read)); len(got) != 2 || got[1] != "lsptest/deco.Timed" {
		t.Fatalf("after an edit, found %v", got)
	}
	if scanned() == first {
		t.Fatal("a changed file wasn't read again")
	}
}

// TestBuildReader: a buffer is made completable once a build, each reader
// getting its own copy, with its hunks; a file not open is read from disk.
func TestBuildReader(t *testing.T) {
	dir := t.TempDir()
	open, closed := filepath.Join(dir, "a.vuka"), filepath.Join(dir, "b.vuka")
	os.WriteFile(closed, []byte("package a\n"), 0o644)
	buf := "package a\n\nfunc f() { x := strings.\n}\n"
	hunks := map[string][]hunk{}
	read := buildReader(map[string][]byte{open: []byte(buf)}, map[string]int{open: len("package a\n\nfunc f() { x := strings.")}, hunks)
	first, _ := read(open)
	if _, ok := hunks[open]; !ok || string(first) == buf {
		t.Fatalf("not made completable: %q, hunks %v", first, hunks)
	}
	again, _ := read(open)
	if string(again) != string(first) || &again[0] == &first[0] {
		t.Fatal("a second read isn't its own copy of the first")
	}
	if b, err := read(closed); err != nil || string(b) != "package a\n" {
		t.Fatalf("from disk: %q %v", b, err)
	}
}
