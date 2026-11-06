package main

import (
	"fmt"
	"strings"
)

// The element an event is on, in an on… attribute's {…}: this.value,
// this.checked and this.<attr> are the reads a method call there sends
// (vuka#353). Completion after this. offers them and the element's own
// attributes; hover says what each reads.

const thisDoc = "`this` is the element the event is on. Its reads are made in the browser when the event fires " +
	"and sent to the method the attribute calls: `this.value`, `this.checked`, `this.<attr>` (an attribute's text, " +
	"`this.dataId` for `data-id`)."

var thisReads = map[string]string{
	"value":   "The element's value when the event fires (`ui.This.Value()`): its text, converted to the method's parameter, a string, number or bool.",
	"checked": "Whether the element is checked when the event fires (`ui.This.Checked()`), for a bool parameter.",
}

// eventRequest answers completion after this. and hover on this and its
// reads, in an on… attribute's value.
func (p *proxy) eventRequest(method string, vf *vfile, off int) func() any {
	if !strings.HasSuffix(vf.source, ".vuka.html") || method != "textDocument/completion" && method != "textDocument/hover" {
		return nil
	}
	src := vf.text()
	t, ok := eventHoleAt(src, off)
	if !ok {
		return nil
	}
	if method == "textDocument/completion" {
		s := off
		for s > 0 && isIdentByte(src[s-1]) {
			s--
		}
		if !thisBefore(src, s) {
			return nil
		}
		return func() any { return thisItems(t, src, s, off) }
	}
	s, e := off, off
	for s > 0 && isIdentByte(src[s-1]) {
		s--
	}
	for e < len(src) && isIdentByte(src[e]) {
		e++
	}
	word := string(src[s:e])
	switch {
	case word == "this" && (s == 0 || src[s-1] != '.') && e < len(src) && src[e] == '.':
		return func() any { return markdownHover(thisDoc, src, s, e) }
	case word != "" && thisBefore(src, s):
		doc, ok := thisReads[word]
		if !ok {
			doc = fmt.Sprintf("The element's `%s` attribute when the event fires (`ui.This.Attr(%q)`): its text, for a string or bool parameter.", thisAttrName(word), thisAttrName(word))
		}
		return func() any { return markdownHover("```go\nthis."+word+"\n```\n\n"+doc, src, s-len("this."), e) }
	}
	return nil
}

// eventHoleAt is the tag whose on… attribute's {…} holds off.
func eventHoleAt(src []byte, off int) (jsxTag, bool) {
	t, ok := tagAround(src, off)
	if !ok || t.close {
		return t, false
	}
	for _, a := range t.attrs {
		if a.expr && isEventName(a.name) && off > a.valStart && off < a.valEnd {
			return t, true
		}
	}
	return t, false
}

func isEventName(name string) bool { return len(name) > 2 && name[:2] == "on" && isLetter(name[2]) }

// thisBefore reports whether this. is written just before s.
func thisBefore(src []byte, s int) bool {
	return s >= 5 && string(src[s-5:s]) == "this." && (s == 5 || !isIdentByte(src[s-6]) && src[s-6] != '.')
}

// thisItems are the reads of the element: its value, whether it is
// checked, and its attributes.
func thisItems(t jsxTag, src []byte, from, off int) any {
	typed := lspRange{positionOf(src, from), positionOf(src, off)}
	item := func(label, detail, doc string, sort int) map[string]any {
		return map[string]any{"label": label, "kind": 10, "detail": detail, "sortText": fmt.Sprintf("%03d", sort),
			"textEdit": map[string]any{"range": typed, "newText": label}, "documentation": map[string]any{"kind": "markdown", "value": doc}}
	}
	items := []any{
		item("value", "ui.This.Value()", thisReads["value"], 0),
		item("checked", "ui.This.Checked()", thisReads["checked"], 1),
	}
	seen := map[string]bool{"value": true, "checked": true}
	for _, a := range t.attrs {
		name := camelAttr(a.name)
		if name == "" || seen[name] || isEventName(a.name) {
			continue
		}
		seen[name] = true
		items = append(items, item(name, fmt.Sprintf("ui.This.Attr(%q)", a.name), fmt.Sprintf("The element's `%s` attribute when the event fires.", a.name), len(items)))
	}
	return map[string]any{"isIncomplete": false, "items": items}
}

// camelAttr is how this.<name> spells an HTML attribute: data-tag is
// dataTag; "" for a name it can't (bind:value, class:x).
func camelAttr(attr string) string {
	if attr == "" || strings.ContainsAny(attr, ":.") || attr == "ref" || attr == "key" {
		return ""
	}
	var b strings.Builder
	up := false
	for _, r := range attr {
		switch {
		case r == '-':
			up = true
		case up && 'a' <= r && r <= 'z':
			b.WriteRune(r - 'a' + 'A')
			up = false
		default:
			b.WriteRune(r)
			up = false
		}
	}
	return b.String()
}

// thisAttrName is the attribute this.<name> reads: dataId is data-id.
func thisAttrName(name string) string {
	var b strings.Builder
	for i, r := range name {
		if 'A' <= r && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}
