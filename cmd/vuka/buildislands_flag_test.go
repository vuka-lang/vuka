package main

import (
	"slices"
	"strings"
	"testing"
)

func TestCutIslands(t *testing.T) {
	for _, c := range []struct {
		cmd  string
		args []string
		want []string
		ok   bool
	}{
		{"build", []string{"-islands"}, []string{}, true},
		{"build", []string{"-islands", "-watch"}, []string{"-watch"}, true},
		{"build", []string{"--islands"}, []string{}, true},
		{"build", []string{"-o", "x", "."}, []string{"-o", "x", "."}, false},
		{"test", []string{"-islands"}, []string{"-islands"}, false},
	} {
		got, ok := cutIslands(c.cmd, c.args)
		if ok != c.ok || !slices.Equal(got, c.want) {
			t.Errorf("cutIslands(%s, %v) = %v, %v", c.cmd, c.args, got, ok)
		}
	}
}

// The old name still works and says so, except where web.Dev starts it.
func TestBuildIslandsDeprecated(t *testing.T) {
	var said strings.Builder
	deprecateBuildIslands(nil, &said)
	if !strings.Contains(said.String(), "vuka build -islands") {
		t.Errorf("got %q", said.String())
	}
	said.Reset()
	deprecateBuildIslands([]string{"-watch"}, &said)
	if said.Len() > 0 {
		t.Errorf("under -watch: %q", said.String())
	}
}
