package check

import (
	"strconv"
	"strings"
)

// What a hole is, by where it sits in a ui.Template.
const (
	kChild = iota // between tags
	kText         // inside title, textarea or script
	kValue        // an attribute's whole value
	kPart         // part of an attribute's value
	kSet          // where an attribute goes
)

// slot is one place of a hole.
type slot struct {
	Hole  int
	Kind  int
	Where string
	Event bool // an on… attribute
	Class bool
	Style bool
}

// holes reads a template's holes as ui.Template parses them: their count
// and places. ok is false for markup ui.Template would refuse at init,
// which the app reports itself.
func holes(src string) (n int, slots []slot, ok bool) {
	h := &holeScan{src: src}
	defer func() {
		if recover() != nil {
			n, slots, ok = 0, nil, false
		}
	}()
	h.nodes("")
	return h.n, h.slots, true
}

type holeScan struct {
	src   string
	i     int
	n     int
	slots []slot
}

type badMarkup struct{}

func (h *holeScan) fail() { panic(badMarkup{}) }

func (h *holeScan) at(s string) bool { return strings.HasPrefix(h.src[h.i:], s) }

func (h *holeScan) hole() (int, bool) {
	s := h.src[h.i:]
	if !strings.HasPrefix(s, "{{") {
		return 0, false
	}
	j := 2
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == 2 || j > 6 || !strings.HasPrefix(s[j:], "}}") {
		return 0, false
	}
	n, _ := strconv.Atoi(s[2:j])
	h.i += j + 2
	h.n = max(h.n, n+1)
	return n, true
}

func (h *holeScan) add(s slot) { h.slots = append(h.slots, s) }

func nameChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-_:.", c) >= 0
}

func (h *holeScan) name() string {
	j := h.i
	for j < len(h.src) && nameChar(h.src[j]) {
		j++
	}
	s := h.src[h.i:j]
	h.i = j
	return s
}

func (h *holeScan) space() {
	for h.i < len(h.src) && strings.IndexByte(" \t\r\n", h.src[h.i]) >= 0 {
		h.i++
	}
}

func (h *holeScan) nodes(parent string) {
	for h.i < len(h.src) {
		switch {
		case h.at("</"):
			return
		case h.at("<!--"):
			end := strings.Index(h.src[h.i+4:], "-->")
			if end < 0 {
				h.fail()
			}
			h.i += 4 + end + 3
		case h.at("<!"):
			end := strings.IndexByte(h.src[h.i:], '>')
			if end < 0 {
				h.fail()
			}
			h.i += end + 1
		case h.i+1 < len(h.src) && h.src[h.i] == '<' && (h.src[h.i+1] >= 'a' && h.src[h.i+1] <= 'z' || h.src[h.i+1] >= 'A' && h.src[h.i+1] <= 'Z'):
			h.element()
		default:
			if n, ok := h.hole(); ok {
				where := "a child"
				if parent != "" {
					where = "a child of <" + parent + ">"
				}
				h.add(slot{Hole: n, Kind: kChild, Where: where})
				continue
			}
			h.i++
		}
	}
}

var void = map[string]bool{"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true}

func (h *holeScan) element() {
	h.i++
	tag := h.name()
	closed := false
	for {
		h.space()
		if h.i >= len(h.src) {
			h.fail()
		}
		if h.at(">") {
			h.i++
			break
		}
		if h.at("/>") {
			h.i += 2
			closed = true
			break
		}
		if n, ok := h.hole(); ok {
			h.add(slot{Hole: n, Kind: kSet, Where: "an attribute set of <" + tag + ">"})
			continue
		}
		h.attr(tag)
	}
	if closed || void[tag] {
		return
	}
	switch tag {
	case "script", "style", "textarea", "title":
		end := strings.Index(h.src[h.i:], "</"+tag)
		if end < 0 {
			h.fail()
		}
		end += h.i
		for h.i < end {
			if n, ok := h.hole(); ok {
				h.add(slot{Hole: n, Kind: kText, Where: "text in <" + tag + ">"})
				continue
			}
			h.i++
		}
	default:
		h.nodes(tag)
	}
	if !h.at("</" + tag) {
		h.fail()
	}
	h.i += 2 + len(tag)
	h.space()
	if !h.at(">") {
		h.fail()
	}
	h.i++
}

func (h *holeScan) attr(tag string) {
	name := h.name()
	if name == "" {
		h.fail()
	}
	save := h.i
	h.space()
	if !h.at("=") {
		h.i = save
		return
	}
	h.i++
	h.space()
	lower := strings.ToLower(name)
	s := slot{Kind: kValue, Where: name + " on <" + tag + ">", Event: len(lower) > 2 && strings.HasPrefix(lower, "on"),
		Class: lower == "class" || name == "className", Style: lower == "style"}
	if n, ok := h.hole(); ok {
		s.Hole = n
		h.add(s)
		return
	}
	if h.i >= len(h.src) {
		h.fail()
	}
	q := h.src[h.i]
	if q != '"' && q != '\'' {
		for h.i < len(h.src) && strings.IndexByte(" \t\r\n>", h.src[h.i]) < 0 && !h.at("/>") {
			h.i++
		}
		return
	}
	h.i++
	end := strings.IndexByte(h.src[h.i:], q)
	if end < 0 {
		h.fail()
	}
	end += h.i
	var found []int
	static := false
	for h.i < end {
		if n, ok := h.hole(); ok {
			found = append(found, n)
			continue
		}
		static = true
		h.i++
	}
	h.i = end + 1
	if len(found) == 1 && !static {
		s.Hole = found[0]
		h.add(s)
		return
	}
	s.Kind, s.Where = kPart, "part of "+s.Where
	for _, n := range found {
		s.Hole = n
		h.add(s)
	}
}
