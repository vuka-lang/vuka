package main

import (
	"slices"
	"testing"
)

// TestGoFlagsWith: the orm tool's GOFLAGS is the go command's, then the
// watch build's compiler flags, each one word as the go command splits
// GOFLAGS, a flag's separate value joined to it.
func TestGoFlagsWith(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GOFLAGS", "-mod=mod '-ldflags=-s -w'")
	goFlagsEnv.Delete(root)
	got, ok := goFlagsWith(root, []string{"-gcflags=example.com/app/...=-l -dwarf=false", "-tags", "a,b", "-trimpath", "-asmflags", "-x"})
	if !ok {
		t.Fatal("not ok")
	}
	words, ok := splitGoFlags(got)
	want := []string{"-mod=mod", "-ldflags=-s -w", "-gcflags=example.com/app/...=-l -dwarf=false", "-tags=a,b", "-trimpath", "-asmflags=-x"}
	if !ok || !slices.Equal(words, want) {
		t.Fatalf("GOFLAGS %q splits into %q, want %q", got, words, want)
	}
	if _, ok := goFlagsWith(root, []string{`-gcflags=-d='x' "y"`}); ok {
		t.Error("a value with both quotes can't be in GOFLAGS")
	}
}
