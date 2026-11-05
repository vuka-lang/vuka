package main

import (
	"io"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// moduleOverlay adds to a transpiled module — gens, through overlay, with
// its route tables — the Go the build generates for the module as a whole
// rather than a package at a time: the link files of its mains (autoLink),
// the orm's typed fields its packages' own pass left out (with tests, those
// of test files' models too) and the call adapters of its packages written
// in Go. vuka's go commands and its language server both take it, so the
// Go an editor's completion reads is the Go the build compiles. A step's
// failure the build can go on without is said on stderr.
func moduleOverlay(root, modPath, tmp, overlay string, pkgs []*load.Package, gens []load.Generated, tests bool, stderr io.Writer) (string, []load.Generated, string, error) {
	t0 := time.Now()
	overlay, gens, linkMsg, err := autoLink(root, modPath, tmp, overlay, pkgs, gens)
	if err != nil {
		return overlay, gens, "", err
	}
	traceStep("module overlay: link %v", time.Since(t0))
	t0 = time.Now()
	overlay, gens = typedFields(root, tmp, overlay, gens, tests, stderr)
	traceStep("module overlay: typed fields %v", time.Since(t0))
	t0 = time.Now()
	defer func() { traceStep("module overlay: call adapters %v", time.Since(t0)) }()
	if calls := newGens(gens, goCallAdapters(root)); len(calls) > 0 {
		if overlay, err = addToOverlay(overlay, tmp, calls); err != nil {
			return overlay, gens, "", err
		}
		gens = append(gens, calls...)
	}
	return overlay, gens, linkMsg, nil
}
