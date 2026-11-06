package main

import (
	"go/format"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// A template's script moves to the script file beside it — orders.go, or
// orders.vuka when it holds what only vuka reads (decorators, field
// attributes) — leaving the template its markup, and back (vuka#356).

// scriptMoveActions are the actions moving a template's script out to its
// script file, from inside the script, and in from it, anywhere in a
// template of markup alone.
func scriptMoveActions(r actionRequest) []any {
	parts, err := transpile.SplitTemplate(r.src)
	if err != nil {
		return nil
	}
	if parts.IsScript() {
		if r.start < parts.Script.Open || r.start > parts.Script.CloseEnd || parts.Markup < 0 || scriptBeside(r.path, r.read) != "" {
			return nil
		}
		c, to := scriptOut(r.path, r.src, parts)
		if c == nil {
			return nil
		}
		return []any{codeAction{Title: "Move the script to " + filepath.Base(to), Kind: "refactor.move", Edit: c.workspaceEdit(r.uri)}}
	}
	from := scriptBeside(r.path, r.read)
	if from == "" || transpile.ParseTemplateHead(r.src).Package != "" {
		return nil
	}
	if sh, _ := transpile.ParseShapeFile(r.path, r.src); sh != nil {
		return nil
	}
	script, rerr := r.read(from)
	if rerr != nil {
		return nil
	}
	c := scriptIn(r.path, r.src, from, script)
	return []any{codeAction{Title: "Move the script from " + filepath.Base(from) + " into this file", Kind: "refactor.move", Edit: c.workspaceEdit(r.uri)}}
}

// scriptBeside is the script file beside the template path, "" when there
// is none.
func scriptBeside(path string, read load.ReadFunc) string {
	for _, name := range transpile.ScriptFileNames(path) {
		if _, err := read(name); err == nil {
			return name
		}
	}
	return ""
}

// scriptOut moves the template's script to its script file: a .go file
// when the script is Go, else a .vuka one. Each file keeps the imports its
// code uses; the markup takes the script file's from the package, so the
// template keeps only those no Go of it uses (an island's ./x.vue).
func scriptOut(path string, src []byte, parts transpile.TemplateParts) (change, string) {
	goText := strings.TrimLeft(string(src[parts.GoStart:parts.GoEnd]), "\n")
	markup := string(src[parts.Markup:])
	imps := readImports([]byte(goText))
	if imps.pkg == "" {
		return nil, ""
	}
	code := goText[imps.end:]
	var goSpecs, markupSpecs []string
	for _, s := range imps.specs {
		spec := strconv.Quote(s.path)
		if s.name != "" {
			spec = s.name + " " + spec
		}
		name := s.localName(nil)
		switch {
		case strings.HasPrefix(s.path, "./") || strings.HasPrefix(s.path, "../"):
			markupSpecs = append(markupSpecs, spec)
		case qualifies(code, name) || !qualifies(markup, name):
			goSpecs = append(goSpecs, spec)
		default:
			markupSpecs = append(markupSpecs, spec)
		}
	}
	script := "package " + imps.pkg + "\n" + scriptImports(goSpecs) + code
	to := strings.TrimSuffix(path, ".vuka.html") + ".go"
	if _, err := parser.ParseFile(token.NewFileSet(), "", script, parser.SkipObjectResolution); err != nil {
		to = strings.TrimSuffix(path, ".vuka.html") + ".vuka"
	} else if b, err := format.Source([]byte(script)); err == nil {
		script = string(b)
	}
	template := markup
	if len(markupSpecs) > 0 {
		template = strings.TrimPrefix(scriptImports(markupSpecs), "\n") + "\n" + markup
	}
	return change{
		{path: path, old: src, edits: []edit{{0, len(src), template}}},
		created(to, script),
	}, to
}

// scriptIn moves the script file from's text into the template of markup
// alone at path, its imports joined with the template's, and deletes from.
func scriptIn(path string, src []byte, from string, script []byte) change {
	imps := readImports(script)
	var specs []string
	seen := map[string]bool{}
	add := func(name, p string) {
		if seen[p] {
			return
		}
		seen[p] = true
		spec := strconv.Quote(p)
		if name != "" {
			spec = name + " " + spec
		}
		specs = append(specs, spec)
	}
	for _, s := range imps.specs {
		add(s.name, s.path)
	}
	head := transpile.ParseTemplateHead(src)
	for _, i := range head.Imports {
		add(i.Name, i.Path)
	}
	lines := strings.SplitAfter(string(src), "\n")
	markup := strings.Join(lines[min(head.Top, len(lines)):], "")
	code := strings.TrimLeft(string(script[imps.end:]), "\n")
	text := "<script lang=\"go\">\npackage " + imps.pkg + "\n" + scriptImports(specs) + "\n" + strings.TrimRight(code, "\n") + "\n</script>\n\n" + markup
	return change{
		{path: path, old: src, edits: []edit{{0, len(src), text}}},
		{path: from, remove: true},
	}
}

// scriptImports is an import declaration of specs after a blank line, ""
// for none.
func scriptImports(specs []string) string {
	switch len(specs) {
	case 0:
		return ""
	case 1:
		return "\nimport " + specs[0] + "\n"
	}
	return "\nimport (\n\t" + strings.Join(specs, "\n\t") + "\n)\n"
}

// qualifies reports whether text names something of the package name:
// name.X.
func qualifies(text, name string) bool {
	return regexp.MustCompile(`(^|[^\w.])` + regexp.QuoteMeta(name) + `\s*\.\s*[A-Za-z_]`).MatchString(text)
}
