package main

import "github.com/vuka-lang/vuka/v2/transpile"

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
