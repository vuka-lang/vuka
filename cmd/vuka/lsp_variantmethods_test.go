package main

import "testing"

// TestVariantDeclsNoAnswer: a definition gopls answers with null (or
// anything not a location) passes through.
func TestVariantDeclsNoAnswer(t *testing.T) {
	p := &proxy{virtual: map[string]*vfile{}, shown: map[string]string{}}
	for _, v := range []any{nil, "x", 1.0, []any{}} {
		if got := p.withVariantDecls(v, nil, 0); got == nil && v != nil {
			t.Errorf("withVariantDecls(%v) = nil", v)
		}
	}
}
