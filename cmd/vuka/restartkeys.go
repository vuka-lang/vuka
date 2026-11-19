package main

import (
	"go/token"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// restartKeys puts the registration of the module's restart-only keys into
// each main package through the overlay, so web.NeedsRestart knows them
// (load.RestartKeys). Nothing is added to a module that reads no key.
func restartKeys(root, modPath, tmp, overlay string, pkgs []*load.Package, gens []load.Generated) (string, []load.Generated, error) {
	files, _, err := load.RestartKeys(root, modPath, pkgs, gens)
	if err != nil || len(files) == 0 {
		return overlay, gens, err
	}
	overlay, err = addToOverlay(overlay, tmp, files)
	if err != nil {
		return "", nil, err
	}
	return overlay, append(gens, files...), nil
}

// restartDiags is W0405 when the analysis met environment reads whose key
// it couldn't read: it names the first and the count.
func restartDiags(rep load.RestartReport) []diag.Diagnostic {
	n := len(rep.Unclassified)
	if n == 0 {
		return nil
	}
	d := diag.Warn("W0405", "%d web.Env %s a key that isn't a string constant: a change to %s isn't reported as needing a restart",
		n, countWord(n, "read has", "reads have"), countWord(n, "that variable", "those variables")).
		WithHelp("write the key as a constant, or handle its changes with web.Hot or web.OnEnvChange")
	d.Span = positionSpan(rep.Unclassified[0])
	for _, p := range rep.Unclassified[1:min(n, 5)] {
		d.Related = append(d.Related, positionSpan(p))
	}
	return []diag.Diagnostic{*d}
}

func positionSpan(p token.Position) diag.Span {
	return diag.Span{File: p.Filename, Line: p.Line, Col: p.Column}
}
