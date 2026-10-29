package main

import (
	"regexp"
	"strings"

	"github.com/vuka-lang/vuka/v2/transpile"
)

// A match's patterns (transpile's match.go): hover on an Ok, Err, Some or
// None says what it matches, and completion after case offers the
// patterns that fit the subject. The bindings themselves are gopls's: the
// Go declares them with the names copied from the patterns.

// matchCtorAt is the Ok, Err, Some or None whose name holds off in f's text.
func (f *vfile) matchCtorAt(off int) (transpile.MatchCtor, bool) {
	from := f.fromOff(off)
	for _, m := range f.matches {
		for _, c := range m.Ctors {
			if from >= c.Off && from < c.End && !f.goneAt(c.Off) {
				return c, true
			}
		}
	}
	return transpile.MatchCtor{}, false
}

// matchCtorDoc is an Ok, Err, Some or None's hover.
func matchCtorDoc(c transpile.MatchCtor) string {
	call := strings.HasPrefix(c.Of, "(")
	doc := "`" + c.Name + "` of `" + c.Of + "`: "
	switch {
	case c.Name == "Ok" && call:
		return doc + "matches when the error is nil, binding the `" + c.Binds + "`."
	case c.Name == "Err" && call:
		return doc + "matches when the error isn't nil, binding it."
	case c.Name == "Ok":
		return doc + "matches a success, binding its `" + c.Binds + "`."
	case c.Name == "Err":
		return doc + "matches a failure, binding its `error`."
	case c.Name == "Some":
		return doc + "matches a value, binding its `" + c.Binds + "`."
	}
	return doc + "matches no value."
}

// casePrefix is a pattern begun after case, up to the cursor: after a
// dot, gopls completes the selector (an enum's variants).
var casePrefix = regexp.MustCompile(`(?:^|\n)[ \t]*case[ \t]+(\w*)$`)

// matchRequest answers hover on a pattern's Ok, Err, Some or None and
// completion after case in a match; nil when off is on neither. p.mu
// must be held.
func (p *proxy) matchRequest(method string, f *vfile, off int) func() any {
	text := f.text()
	switch method {
	case "textDocument/hover":
		c, ok := f.matchCtorAt(off)
		if !ok {
			return nil
		}
		start, end := f.curOff(c.Off), f.curOff(c.End)
		return func() any { return markdownHover(matchCtorDoc(c), text, start, end) }
	case "textDocument/completion":
		ls := strings.LastIndexByte(string(text[:off]), '\n') + 1
		sm := casePrefix.FindSubmatchIndex(text[ls:off])
		if sm == nil {
			return nil
		}
		from := f.fromOff(off)
		for _, m := range f.matches {
			if from <= m.LBrace || from > m.RBrace || f.goneAt(m.LBrace) {
				continue
			}
			typed := lspRange{positionOf(text, ls+sm[2]), positionOf(text, off)}
			colon := off < len(text) && strings.HasPrefix(strings.TrimLeft(string(text[off:]), " \t"), ":")
			items := []any{}
			for i, s := range m.Cases {
				insert := s
				if !colon {
					insert += ":"
				}
				label := snippetLabel(s)
				kind := 4 // a constructor
				if strings.Contains(label, ".") {
					kind = 20
				} else if label == "_" {
					kind = 14
				}
				items = append(items, map[string]any{"label": label, "kind": kind, "detail": "pattern of " + m.Subject,
					"sortText": string(rune('a' + i)), "filterText": label, "insertTextFormat": 2,
					"textEdit": map[string]any{"range": typed, "newText": insert}})
			}
			return func() any { return map[string]any{"isIncomplete": false, "items": items} }
		}
	}
	return nil
}

var snippetHole = regexp.MustCompile(`\$\{\d+(?::([^}]*))?\}`)

// snippetLabel is a snippet as it reads: Ok(${1:user}) → Ok(user),
// Shape.Circle{${1}} → Shape.Circle{…}.
func snippetLabel(s string) string {
	return snippetHole.ReplaceAllStringFunc(s, func(h string) string {
		if m := snippetHole.FindStringSubmatch(h); m[1] != "" {
			return m[1]
		}
		return "…"
	})
}

// diagFixes are the quick fixes vuka's diagnostics in the request carry
// (diagnosticFix), whose text is still what they were made for.
func diagFixes(r actionRequest) []any {
	var out []any
	for _, d := range r.diags {
		data, _ := d["data"].(map[string]any)
		fx, _ := data["vukaFix"].(map[string]any)
		title, _ := fx["title"].(string)
		list, _ := fx["edits"].([]any)
		dr, ok := toRange(d["range"])
		if title == "" || len(list) == 0 || !ok || !r.touches(offsetOf(r.src, dr.Start), offsetOf(r.src, dr.End)) {
			continue
		}
		var edits []edit
		for _, x := range list {
			e, _ := x.(map[string]any)
			er, ok := toRange(e["range"])
			text, _ := e["newText"].(string)
			old, _ := e["old"].(string)
			s, en := offsetOf(r.src, er.Start), offsetOf(r.src, er.End)
			if !ok || en < s || string(r.src[s:en]) != old {
				edits = nil
				break
			}
			edits = append(edits, edit{s, en, text})
		}
		if len(edits) == 0 {
			continue
		}
		out = append(out, codeAction{Title: title, Kind: "quickfix", IsPreferred: true, Diagnostics: []map[string]any{d},
			Edit: change{{path: r.path, old: r.src, edits: edits}}.workspaceEdit(r.uri)})
	}
	return out
}

// diagnosticFix gives d the fix e carries, for diagFixes: its edits in
// text, curOff mapping an offset of the file e is of onto it.
func diagnosticFix(d lspDiagnostic, e *transpile.Error, text []byte, curOff func(int) int) lspDiagnostic {
	if e.FixTitle == "" || len(e.Edits) == 0 {
		return d
	}
	var edits []any
	for _, x := range e.Edits {
		s, en := curOff(x.Off), curOff(x.End)
		if s > en || en > len(text) {
			return d
		}
		edits = append(edits, map[string]any{"range": lspRange{positionOf(text, s), positionOf(text, en)},
			"newText": x.Text, "old": string(text[s:en])})
	}
	d.Data = map[string]any{"vukaFix": map[string]any{"title": e.FixTitle, "edits": edits}}
	return d
}

// hintsInSource moves gopls's inlay hints on f's Go onto its source: a
// hint next to code copied from the source goes next to it there (a
// match binding's type after its pattern's name); one only next to code
// Vuka wrote goes.
func hintsInSource(v any, f *vfile) any {
	list, ok := v.([]any)
	if !ok || f == nil || f.m == nil {
		return v
	}
	out := []any{}
	for _, h := range list {
		hm, _ := h.(map[string]any)
		pos, ok := toPosition(hm["position"])
		if !ok {
			continue
		}
		g := offsetOf(f.gen, pos)
		s, exact, ok := f.m.ToSource(g - 1)
		if g > 0 && ok && exact {
			s++
		} else if s, exact, ok = f.m.ToSource(g); !ok || !exact {
			continue
		}
		if f.goneAt(s) {
			continue
		}
		hm["position"] = positionOf(f.text(), f.curOff(s))
		delete(hm, "textEdits")
		out = append(out, hm)
	}
	return out
}
