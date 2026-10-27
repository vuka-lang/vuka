package main

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// Organize imports in the editor: source.organizeImports on a .vuka or
// .vuka.html file is vuka's (organize.go), never gopls's, whose would
// organize the generated Go. A name used with several packages it can mean
// gets no import; the diagnostic about it offers each as a quick fix.

// kindOrganize is the code action kind organizing a file's imports.
const kindOrganize = "source.organizeImports"

// organizeWait is how long the editor's request waits for the imports
// organized: an editor organizing on save saves without them after that.
var organizeWait = 2 * time.Second

// organizeActions is the organize imports action for the request's file,
// when its imports change; none when working them out takes too long.
func organizeActions(r actionRequest) []any {
	ch := make(chan *importEdit, 1)
	go func() {
		o, err := organizeImports(r.path, r.src, r.read, r.ctxVars)
		if err != nil {
			ch <- nil
			return
		}
		ch <- o.edit
	}()
	var e *importEdit
	select {
	case e = <-ch:
	case <-time.After(organizeWait):
	}
	if e == nil {
		return nil
	}
	c := change{{path: r.path, old: r.src, edits: []edit{{e.start, e.end, e.text}}}}
	return []any{codeAction{Title: "Organize imports", Kind: kindOrganize, Edit: c.workspaceEdit(r.uri)}}
}

// contextVarNames are the names of the context processors' variables the
// files built so far may read; nil before any is built.
func (p *proxy) contextVarNames() map[string]bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out map[string]bool
	for _, vf := range p.virtual {
		if vf == nil || vf.isTempl() {
			continue
		}
		if out == nil {
			out = map[string]bool{}
		}
		for _, v := range vf.avail {
			out[v.Name] = true
		}
		for _, v := range vf.vars {
			out[v.Name] = true
		}
	}
	return out
}

// organizeOnly reports whether a code action request asks for nothing but
// organizing imports (as an editor organizing on save does): vuka answers
// it alone.
func organizeOnly(only []string) bool {
	if len(only) == 0 {
		return false
	}
	for _, o := range only {
		if o != kindOrganize && o != "source" {
			return false
		}
	}
	return true
}

var (
	undefinedName  = regexp.MustCompile(`^undefined: (\w+)$`)
	unimportedName = regexp.MustCompile(`imports no package named (\w+)`)
)

// importFixes are the quick fixes adding an import for the request's
// diagnostics about a package name with no import (gopls's undefined, an
// attribute's E0215): one for each package it can mean, as organizing
// imports finds them; when it can mean several, organizing adds none.
func importFixes(r actionRequest) []any {
	var out []any
	var scope *transpile.ImportScope
	for _, d := range r.diags {
		msg, _ := d["message"].(string)
		code, _ := d["code"].(string)
		var name string
		if m := undefinedName.FindStringSubmatch(strings.TrimSpace(strings.SplitN(msg, "\n", 2)[0])); m != nil {
			name = m[1]
		} else if m := unimportedName.FindStringSubmatch(msg); m != nil && code == "E0215" {
			name = m[1]
		}
		if name == "" {
			continue
		}
		if scope == nil {
			s, _ := packageScope(r.path, r.read)
			scope = &s
		}
		cands := removeString(scope.Candidates(name), ownImportPath(*scope, filepath.Dir(r.path)))
		for _, p := range cands {
			eds := addImports(transpile.HeaderView(r.src), []importSpec{{path: p}})
			if len(eds) == 0 {
				continue
			}
			var es []edit
			for _, e := range eds {
				es = append(es, edit{e.start, e.end, e.text})
			}
			c := change{{path: r.path, old: r.src, edits: es}}
			out = append(out, codeAction{Title: "Add import " + strconv.Quote(p), Kind: "quickfix",
				Diagnostics: []map[string]any{d}, IsPreferred: len(cands) == 1, Edit: c.workspaceEdit(r.uri)})
		}
	}
	return out
}

// withoutGoplsOrganize is gopls's code actions for a .vuka file without its
// organize imports and its quick fixes adding an import, which work on the
// generated Go and pick one package of several: vuka's own replace them.
func withoutGoplsOrganize(v any) any {
	list, ok := v.([]any)
	if !ok {
		return v
	}
	out := list[:0:0]
	for _, a := range list {
		if m, ok := a.(map[string]any); ok {
			title, _ := m["title"].(string)
			if m["kind"] == kindOrganize || m["kind"] == "quickfix" && strings.HasPrefix(title, "Add import: ") {
				continue
			}
		}
		out = append(out, a)
	}
	return out
}
