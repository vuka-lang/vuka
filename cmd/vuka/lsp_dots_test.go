package main

import (
	"slices"
	"strings"
	"testing"
)

func TestDanglingDots(t *testing.T) {
	for src, want := range map[string][]int{
		"x := u.\n}":                          {7},
		"f(u.)":                               {4},
		"x := u.\n\treturn 1":                 {7},
		"var v = ui.JS.\n\tIf(c).\n\tEnd()":   nil,
		"\tfmt.\n\tx := 1":                    {5},
		"s := strings.\n\tToUpper(x)":         nil,
		"\tv := ui.JS.\n\t\tIf(c).\n\tx := 1": {21},
		"a := b.c.\n\tif ok {":                {9},
	} {
		if got := danglingDots([]byte(src)); !slices.Equal(got, want) {
			t.Errorf("danglingDots(%q) = %v, want %v", src, got, want)
		}
	}
}

// TestTemplateTextDots: in a template file's markup, a sentence ending in a
// package's name and a dot (failing to sync.) is text, which takes no
// placeholder — Go a completion would import the package for; a selector
// being typed in a hole, an attribute or a statement still does.
func TestTemplateTextDots(t *testing.T) {
	src := "package orders\n\nimport \"strings\"\n\n// Status says the sync state.\ntype Status struct {\n\tFailed bool\n}\n" +
		"<p title={strings.} data-x=\"in a while.\">\n  if Failed {\n    The orders are failing to sync.\n    n := strings.\n    <b>{strings.\n}</b>\n  }\n  Check strings.\n</p>\n"
	out, _ := completableAt("status.vuka.html", []byte(src), -1)
	got := strings.Count(string(out), placeholder)
	for _, want := range []string{"title={strings." + placeholder + "}", "n := strings." + placeholder, "{strings." + placeholder} {
		if !strings.Contains(string(out), want) {
			t.Errorf("no placeholder in %q:\n%s", want, out)
		}
	}
	if got != 3 || strings.Contains(string(out), "sync."+placeholder) || strings.Contains(string(out), "Check strings."+placeholder) || strings.Contains(string(out), "while."+placeholder) {
		t.Errorf("placeholders in the text (%d in all):\n%s", got, out)
	}
}
