package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGenCheckScope: vuka gen -check looks at the source tree only. build/
// is the build's output, so a stale build/ module is not drift; a stale
// generated file beside its .vuka source is.
func TestGenCheckScope(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := t.TempDir()
	gomod, gosum := testModule(t, "example.com/app")
	writeFile(t, filepath.Join(root, "go.mod"), gomod)
	writeFile(t, filepath.Join(root, "go.sum"), gosum)
	src, err := os.ReadFile("../../examples/users/main.vuka")
	if err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(root, "users", "main.vuka")
	writeFile(t, main, string(src))

	edit := func(old, new string) {
		t.Helper()
		b, err := os.ReadFile(main)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), old) {
			t.Fatalf("users/main.vuka has no %q", old)
		}
		writeFile(t, main, strings.Replace(string(b), old, new, 1))
	}

	var out bytes.Buffer
	if err := vukaIn(root, &out, &out, "gen"); err != nil {
		t.Fatalf("vuka gen: %v\n%s", err, out.Bytes())
	}
	if _, err := os.Stat(filepath.Join(root, "build", ".vuka-build")); err != nil {
		t.Fatalf("vuka gen wrote no build/ module: %v\n%s", err, out.Bytes())
	}
	out.Reset()
	if err := vukaIn(root, &out, &out, "gen", "-check"); err != nil {
		t.Fatalf("vuka gen -check with build/ in sync: %v\n%s", err, out.Bytes())
	}

	// build/ goes stale: not drift.
	edit(`"ada"`, `"ada lovelace"`)
	out.Reset()
	if err := vukaIn(root, &out, &out, "gen", "-check"); err != nil {
		t.Fatalf("vuka gen -check flags the stale build/ module: %v\n%s", err, out.Bytes())
	}
	if strings.Contains(out.String(), "stale") {
		t.Fatalf("vuka gen -check reports build/:\n%s", out.String())
	}

	// A generated file in the source tree goes stale: drift.
	if err := vukaIn(root, io.Discard, &out, "gen", "-inplace", "./users"); err != nil {
		t.Fatalf("vuka gen -inplace: %v\n%s", err, out.Bytes())
	}
	out.Reset()
	if err := vukaIn(root, &out, &out, "gen", "-check"); err != nil {
		t.Fatalf("vuka gen -check right after gen -inplace: %v\n%s", err, out.Bytes())
	}
	edit(`"linus"`, `"linus torvalds"`)
	out.Reset()
	if err := vukaIn(root, &out, &out, "gen", "-check"); err == nil || !strings.Contains(out.String(), "stale: "+filepath.Join("users", "main_vuka.go")) {
		t.Fatalf("vuka gen -check with a stale users/main_vuka.go: %v\n%s", err, out.Bytes())
	}
}
