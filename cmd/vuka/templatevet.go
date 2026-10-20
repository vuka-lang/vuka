package main

import (
	"path/filepath"
	"unicode"
	"unicode/utf8"

	"github.com/vuka-lang/vuka/diag"
	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

// vetTemplates warns of the module's exported templates without a doc
// comment (W0104), as Go's linters do of exported functions. The markup
// methods, View and Render, are documented by their type.
func vetTemplates(root string, pkgs []*load.Package) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, p := range pkgs {
		for _, f := range p.Files {
			if !f.IsTemplate() {
				continue
			}
			file := relPath(root, filepath.Join(p.Dir, f.Name))
			for _, h := range transpile.TemplateHeaders(f.Src) {
				if r, _ := utf8.DecodeRuneInString(h.Name); h.Doc || !unicode.IsUpper(r) || h.Method && (h.Name == "View" || h.Name == "Render") {
					continue
				}
				col := h.Off - lineStartOf(f.Src, h.Off) + 1
				out = append(out, *diag.Warn("W0104", "the exported template %s has no doc comment", h.Name).
					At(diag.Span{File: file, Line: h.Line, Col: col, EndCol: col + len(h.Name)}).
					WithHelp("say what it shows in a comment right above it: // %s …", h.Name))
			}
		}
	}
	return out
}

func lineStartOf(src []byte, off int) int {
	for off > 0 && src[off-1] != '\n' {
		off--
	}
	return off
}
