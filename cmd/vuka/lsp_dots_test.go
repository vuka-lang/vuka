package main

import (
	"slices"
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
