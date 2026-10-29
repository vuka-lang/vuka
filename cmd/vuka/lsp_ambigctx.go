package main

import (
	"strings"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// ambigFixes are the calls of one file's markup that compile both as
// written and with the render's ctx (W0108), each as the edit writing its
// ctx.
func ambigFixes(ws transpile.ErrorList) []edit {
	var out []edit
	for _, w := range ws {
		if w.Code == "W0108" && w.Fix != "" && w.Len > 0 {
			out = append(out, edit{w.Pos.Offset, w.Pos.Offset + w.Len, w.Fix})
		}
	}
	return out
}

// ambigCtxActions are the quick fixes of the W0108 calls the request's
// range touches: write the ctx, calling the overload taking it, or write
// transpile.NoCtxMarker, keeping the one the call compiles to as written.
func ambigCtxActions(r actionRequest) []any {
	var out []any
	for _, fx := range r.ambig {
		if !r.touches(fx.start, fx.end) || fx.end > len(r.src) {
			continue
		}
		call := string(r.src[fx.start:fx.end])
		marked, ok := noCtxRewrite(call, fx.text)
		if !ok {
			continue
		}
		d := r.diag("W0108")
		for i, text := range []string{fx.text, marked} {
			shown := strings.Join(strings.Fields(text), " ")
			title := "Pass the render's ctx: " + shown
			if i == 1 {
				title = "Keep the call without ctx: " + shown
			}
			a := codeAction{Title: title, Kind: "quickfix", IsPreferred: i == 0,
				Edit: change{{path: r.path, old: r.src, edits: []edit{{fx.start, fx.end, text}}}}.workspaceEdit(r.uri)}
			if d != nil {
				a.Diagnostics = []map[string]any{d}
			}
			out = append(out, a)
		}
	}
	return out
}

// noCtxRewrite is call with transpile.NoCtxMarker where withCtx writes the
// ctx: Items(/*vuka:noctx*/) for Items(ctx), Badge(/*vuka:noctx*/ 3) for
// Badge(ctx, 3).
func noCtxRewrite(call, withCtx string) (string, bool) {
	for i := 0; i <= len(call); i++ {
		switch withCtx {
		case call[:i] + "ctx, " + call[i:]:
			return call[:i] + transpile.NoCtxMarker + " " + call[i:], true
		case call[:i] + "ctx" + call[i:]:
			return call[:i] + transpile.NoCtxMarker + call[i:], true
		}
	}
	return "", false
}
