package main

import (
	"regexp"
	"slices"
	"strings"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// vuka vet reads a shape's script for the names it passes as strings, as
// the generated types check them in the editor: a ref, state or event the
// component doesn't have, and ctx.emit on a page, are W0308.

// scriptCall is a call naming one of ctx's lists: ctx.ref("box").
var scriptCall = regexp.MustCompile(`\b(\w+)\.(ref|state|on|emit)\(\s*(['"])([^'"\n]*)['"]`)

// scriptCtxParam is the name a script gives ctx: the third parameter of
// component<T>((el, props, ctx) => …) or page<T>((el, live, ctx) => …).
var scriptCtxParam = regexp.MustCompile(`\b(?:component|page)\s*(?:<[^>]*>)?\s*\(\s*(?:function\s*)?\(\s*\w+\s*,\s*\w+\s*,\s*(\w+)`)

// vetScripts are the W0308s of the shapes' scripts.
func vetScripts(root, modPath string, pa *pageAssets) []diag.Diagnostic {
	var out []diag.Diagnostic
	var pages map[string]bool
	r := newTSTypes(root, modPath)
	for _, c := range pa.comps {
		if c.name == "" || len(c.scripts) == 0 || c.classic {
			continue
		}
		markup, ok := markupOf(c.markup, c.name)
		if !ok {
			continue // its markup is Go or templ: no list is whole
		}
		if pages == nil {
			pages = scriptPages(root, modPath)
		}
		var n scriptNames
		n.readMarkupNames(markup)
		n.readGoNames(r, c.dir, c.name)
		_, states := componentFields(c.dir, c.name)
		page := c.page || pages[c.dir+"\x00"+c.name] || routeDecorated(c.dir, c.name)
		lists := map[string][]string{}
		for _, ref := range n.refs {
			lists["ref"] = append(lists["ref"], ref.name)
		}
		for _, s := range states {
			lists["state"] = append(lists["state"], s[0])
		}
		for _, e := range n.events {
			lists["on"] = append(lists["on"], e.name)
		}
		lists["emit"] = n.emits
		for _, s := range c.scripts {
			src, base := s.inline, s.line
			if s.path != "" {
				b, err := load.ReadSource(s.path)
				if err != nil {
					continue
				}
				src, base = string(b), 0
			}
			code := blankComments(src)
			ctx := "ctx"
			if m := scriptCtxParam.FindStringSubmatch(code); m != nil {
				ctx = m[1]
			}
			span := labelSpan(s.label)
			if s.path != "" {
				span = diag.Span{File: s.label}
			}
			for local, kind := range setupAliases(src) {
				call := regexp.MustCompile(`\b` + regexp.QuoteMeta(local) + `(\.all)?\(\s*(['"])([^'"\n]*)['"]`)
				for _, m := range call.FindAllStringSubmatchIndex(code, -1) {
					if m[0] > 0 && code[m[0]-1] == '.' {
						continue
					}
					name := code[m[6]:m[7]]
					at := span
					at.Line = base + strings.Count(src[:m[6]], "\n") + 1
					at.Col = m[6] - strings.LastIndexByte(src[:m[6]], '\n')
					at.EndCol = at.Col + len(name)
					if d := scriptNameDiag(c.name, kind, name, lists[kind], page, n); d != nil {
						d.Message = strings.Replace(d.Message, "ctx."+kind, local, 1)
						out = append(out, *d.At(at))
					} else if kind == "ref" && m[2] < 0 {
						for _, r := range n.refs {
							if r.name == name && r.loop {
								out = append(out, *diag.Warn("W0308", "%s(%q) names a ref inside a for; use %s.all(%q)", local, name, local, name).At(at))
								break
							}
						}
					}
				}
			}
			for _, m := range scriptCall.FindAllStringSubmatchIndex(code, -1) {
				if code[m[2]:m[3]] != ctx {
					continue
				}
				kind, name := code[m[4]:m[5]], code[m[8]:m[9]]
				at := span
				at.Line = base + strings.Count(src[:m[8]], "\n") + 1
				at.Col = m[8] - strings.LastIndexByte(src[:m[8]], '\n')
				at.EndCol = at.Col + len(name)
				if d := scriptNameDiag(c.name, kind, name, lists[kind], page, n); d != nil {
					out = append(out, *d.At(at))
				}
			}
		}
	}
	return out
}

// scriptNameDiag is the W0308 of ctx.<kind>(name) in a script of typ, nil
// when the name is one of have.
func scriptNameDiag(typ, kind, name string, have []string, page bool, n scriptNames) *diag.Diagnostic {
	if kind == "emit" && page {
		return diag.Warn("W0308", "%s is a page: ctx.emit(%q) has no parent to reach", typ, name).
			WithHelp("call the page's own method, ctx.live.%s(…), or push its event from Go", lowerFirst(name))
	}
	if slices.Contains(have, name) || kind == "ref" && n.dynamicRefs || kind == "on" && n.dynamicEvts {
		return nil
	}
	what := map[string]string{
		"ref":   "no element of %s's markup has ref=%q",
		"state": "%s has no ui.State field %q",
		"on":    "no method of %s calls PushEvent(%q, …)",
		"emit":  "the root of %s's markup has no on… attribute for %q",
	}[kind]
	d := diag.Warn("W0308", "ctx.%s(%q): "+what, kind, name, typ, name)
	if s := closest(name, have); s != "" {
		return d.WithHelp("did you mean %q?", s)
	}
	switch kind {
	case "ref":
		return d.WithHelp(`give the element ref=%q`, name)
	case "state":
		return d.WithHelp("declare the field: %s ui.State[T]", name)
	case "on":
		return d.WithHelp("push it from a method: c.PushEvent(%q, payload)", name)
	}
	return d.WithHelp("put on%s={…} on the component's root element", strings.ToUpper(name[:min(1, len(name))])+name[min(1, len(name)):])
}

var setupImport = regexp.MustCompile(`(?s)\bimport\s*\{([^}]*)\}\s*from\s*['"]vuka:component(?:/[^'"]+)?['"]`)

func setupAliases(src string) map[string]string {
	out := map[string]string{}
	for _, m := range setupImport.FindAllStringSubmatch(blankComments(src), -1) {
		for _, part := range strings.Split(m[1], ",") {
			words := strings.Fields(part)
			if len(words) == 0 {
				continue
			}
			kind := words[0]
			if kind != "ref" && kind != "state" && kind != "on" && kind != "emit" {
				continue
			}
			local := kind
			if len(words) == 3 && words[1] == "as" {
				local = words[2]
			}
			out[local] = kind
		}
	}
	return out
}
