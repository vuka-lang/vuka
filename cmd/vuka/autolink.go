package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vuka-lang/vuka/internal/load"
)

// autoLink puts the link file of every main package of the module — blank
// imports of every package that declares routes, crons or other
// registrations through decorators (load.AutoLink) — into the overlay, so no
// hand-written import is needed for a declarer package to be compiled in. It
// returns the overlay (made when there was none), gens with the link files
// appended, and a one-line message naming what is linked ("" when nothing is).
func autoLink(root, modPath, tmp, overlay string, pkgs []*load.Package, gens []load.Generated) (string, []load.Generated, string, error) {
	links, linked, err := load.AutoLink(root, modPath, pkgs, gens)
	if err != nil || len(links) == 0 {
		return overlay, gens, "", err
	}
	overlay, err = addToOverlay(overlay, tmp, links)
	if err != nil {
		return "", nil, "", err
	}
	return overlay, append(gens, links...), linkMessage(modPath, linked), nil
}

// linkMessage says once what auto-link added: the union of the packages the
// module's mains were given, as paths relative to the module.
func linkMessage(modPath string, linked []load.LinkedMain) string {
	set := map[string]bool{}
	for _, m := range linked {
		for _, p := range m.Imports {
			set[strings.TrimPrefix(p, modPath+"/")] = true
		}
	}
	if len(set) == 0 {
		return ""
	}
	rels := make([]string, 0, len(set))
	for p := range set {
		rels = append(rels, p)
	}
	sort.Strings(rels)
	shown := rels
	if len(shown) > 6 {
		shown = append(shown[:6:6], "…")
	}
	word := "packages"
	if len(rels) == 1 {
		word = "package"
	}
	return fmt.Sprintf("● linking %d declarer %s (%s)", len(rels), word, strings.Join(shown, ", "))
}
