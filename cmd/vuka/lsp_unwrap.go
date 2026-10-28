package main

import (
	"strings"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// A call in markup returning (T, error) is its T there (transpile's
// unwrap.go): hover on its name says so under gopls's signature.

// unwrapAt is the unwrapped call whose name, up to its (, holds off in f's
// text: the innermost, for a call inside another's receiver.
func (f *vfile) unwrapAt(off int) (transpile.Unwrap, bool) {
	from := f.fromOff(off)
	var best transpile.Unwrap
	found := false
	for _, u := range f.unwraps {
		if from >= u.Off && from < u.Lparen && !f.goneAt(u.Off) && (!found || u.Lparen-u.Off < best.Lparen-best.Off) {
			best, found = u, true
		}
	}
	return best, found
}

// unwrapNote is an unwrapped call's hover note.
func unwrapNote(u transpile.Unwrap) string {
	return "In markup this call is its `" + u.Type + "`: an error it returns fails the render, as a `Load` or `Mount` error does. It runs on every render, each live re-render included."
}

// A call in markup one argument short of a context.Context it takes first
// gets the render's ctx (transpile's implicit.go): hover on its name says
// so, and an inlay hint shows the ctx where it goes.

// implicitNote is the hover note of a call given the render's ctx.
const implicitNote = "In markup this call gets the render's `ctx` first: ctx is supplied by the page."

// implicitAt is the call given ctx whose name, up to its (, holds off in
// f's text: the innermost.
func (f *vfile) implicitAt(off int) (transpile.ImplicitCtx, bool) {
	from := f.fromOff(off)
	var best transpile.ImplicitCtx
	found := false
	for _, c := range f.implicit {
		if from >= c.Off && from < c.Lparen && !f.goneAt(c.Off) && (!found || c.Lparen-c.Off < best.Lparen-best.Off) {
			best, found = c, true
		}
	}
	return best, found
}

// implicitHints are f's inlay hints for the ctx its markup's calls get:
// "ctx," after each one's ( (just "ctx" for a call with no arguments).
func (f *vfile) implicitHints() []any {
	var out []any
	src := f.text()
	for _, c := range f.implicit {
		at := c.Lparen + 1
		if f.goneAt(at) {
			continue
		}
		label := "ctx"
		if c.Args {
			label = "ctx,"
		}
		out = append(out, map[string]any{
			"position":     positionOf(src, f.curOff(at)),
			"label":        label,
			"kind":         2, // a parameter
			"paddingRight": c.Args,
			"tooltip":      implicitNote,
		})
	}
	return out
}

// withImplicitHints is gopls's hints v and f's ctx hints: gopls's own for
// the ctx argument it sees in the Go dropped.
func withImplicitHints(v any, f *vfile) any {
	hints := f.implicitHints()
	if len(hints) == 0 {
		return v
	}
	at := map[lspPosition]bool{}
	for _, h := range hints {
		at[h.(map[string]any)["position"].(lspPosition)] = true
	}
	list, _ := v.([]any)
	var out []any
	for _, h := range list {
		m, ok := h.(map[string]any)
		if ok && strings.HasPrefix(hintLabel(m["label"]), "ctx") {
			if pos, ok := m["position"].(map[string]any); ok {
				line, _ := pos["line"].(float64)
				char, _ := pos["character"].(float64)
				if at[lspPosition{Line: uint32(line), Character: uint32(char)}] {
					continue
				}
			}
		}
		out = append(out, h)
	}
	return append(out, hints...)
}

// hintLabel is an inlay hint's label as text: a string, or its parts'.
func hintLabel(l any) string {
	switch l := l.(type) {
	case string:
		return l
	case []any:
		var b strings.Builder
		for _, p := range l {
			if m, ok := p.(map[string]any); ok {
				s, _ := m["value"].(string)
				b.WriteString(s)
			}
		}
		return b.String()
	}
	return ""
}
