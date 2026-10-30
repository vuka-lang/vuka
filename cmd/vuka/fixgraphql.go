package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// graphql-params: an argument's attribute written above its operation or
// field method — @graphql.AsInt("lastID"), @graphql.Arg("after", "cursor")
// — goes before the parameter it is about (W0803). The compiler works out
// each move (transpile.Error.Edits); the fixer and the editor's quick fix
// apply it.
func fixGraphQLParams(m *fixModule) ([]fixChange, error) {
	byFile := map[string][]edit{}
	count := map[string]int{}
	for _, g := range m.gens {
		for _, w := range g.Warnings {
			if w.Code != "W0803" || len(w.Edits) == 0 {
				continue
			}
			file := w.Pos.Filename
			if !filepath.IsAbs(file) {
				file = filepath.Join(filepath.Dir(g.Source), filepath.Base(file))
			}
			for _, e := range w.Edits {
				byFile[file] = append(byFile[file], edit{e.Off, e.End, e.Text})
			}
			count[file]++
		}
	}
	files := make([]string, 0, len(byFile))
	for f := range byFile {
		files = append(files, f)
	}
	sort.Strings(files)
	var out []fixChange
	for _, file := range files {
		src, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		edits := byFile[file]
		rel, _ := filepath.Rel(m.root, file)
		what := fmt.Sprintf("%s: %d attribute(s) written before their parameters", filepath.ToSlash(rel), count[file])
		out = append(out, fixChange{what, func() error {
			return os.WriteFile(file, applySpans(src, edits), 0o644)
		}})
	}
	return out, nil
}

// graphql-args (2.14): a GraphQL argument of a value type is non-null — a
// parameter first int is first: Int!, where earlier releases made it Int
// and gave a request leaving it out the zero value. Whether a client may
// leave one out is the app's to say, so the fixer changes nothing: it lists
// each such argument, to be made a pointer (first *int) where it should
// stay optional.
func fixGraphQLArgs(m *fixModule) ([]fixChange, error) {
	var args []transpile.GraphQLArg
	for _, g := range m.gens {
		for _, a := range g.GraphQLArgs {
			if !filepath.IsAbs(a.Pos.Filename) {
				a.Pos.Filename = filepath.Join(filepath.Dir(g.Source), filepath.Base(a.Pos.Filename))
			}
			args = append(args, a)
		}
	}
	sort.SliceStable(args, func(i, j int) bool {
		if args[i].Pos.Filename != args[j].Pos.Filename {
			return args[i].Pos.Filename < args[j].Pos.Filename
		}
		return args[i].Pos.Offset < args[j].Pos.Offset
	})
	if m.genErr != nil {
		m.notes = append(m.notes, fmt.Sprintf("the module doesn't compile, so its arguments may not all be listed: %v", m.genErr))
	}
	for _, a := range args {
		rel, err := filepath.Rel(m.root, a.Pos.Filename)
		if err != nil {
			rel = a.Pos.Filename
		}
		m.notes = append(m.notes, fmt.Sprintf("%s:%d: %s's argument %s (%s) is required: a request must give it; make it *%s if it may be left out",
			filepath.ToSlash(rel), a.Pos.Line, a.Field, a.Param, a.Type, a.Type))
	}
	if len(args) > 0 {
		m.notes = append(m.notes, fmt.Sprintf("%d argument(s) listed; nothing changed", len(args)))
	}
	return nil, nil
}
