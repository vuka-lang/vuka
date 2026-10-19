package check

import (
	"slices"
	"strings"
)

// What a hole is, by where it sits in ui.HTML's markup.
const (
	kChild  = iota // between tags
	kText          // inside title or textarea
	kValue         // an attribute's whole value
	kPart          // part of an attribute's value
	kSet           // where an attribute goes: {attrs...}
	kToggle        // class:name={on}
)

// slot is one place of a hole: Label is its value's index.
type slot struct {
	Label int
	Kind  int
	Where string
	Event bool // an on… attribute
	Class bool
	Style bool
}

// holes reads ui.HTML's markup as it parses it: the labels, one value each
// in the order they first appear, and the holes' places. ok is false for
// markup the render refuses itself.
func holes(src string) (labels []string, slots []slot, ok bool) {
	h := &holeScan{src: src}
	defer func() {
		if recover() != nil {
			labels, slots, ok = nil, nil, false
		}
	}()
	h.nodes("")
	return h.labels, h.slots, true
}

type holeScan struct {
	src    string
	i      int
	labels []string
	slots  []slot
}

type badMarkup struct{}

func (h *holeScan) fail() { panic(badMarkup{}) }

func (h *holeScan) at(s string) bool { return strings.HasPrefix(h.src[h.i:], s) }

func (h *holeScan) holeAt() bool { return h.i < len(h.src) && h.src[h.i] == '{' && !h.at("{{") }

// hole reads {label} or {label...} at h.i.
func (h *holeScan) hole() (label int, spread bool) {
	end := goEnd(h.src, h.i+1)
	if end < 0 {
		h.fail()
	}
	l := strings.TrimSpace(h.src[h.i+1 : end])
	h.i = end + 1
	if s, ok := strings.CutSuffix(l, "..."); ok {
		l, spread = strings.TrimSpace(s), true
	}
	if l == "" {
		h.fail()
	}
	if i := slices.Index(h.labels, l); i >= 0 {
		return i, spread
	}
	h.labels = append(h.labels, l)
	return len(h.labels) - 1, spread
}

// goEnd is the } closing the Go text at s[i:], past brackets and literals.
func goEnd(s string, i int) int {
	depth := 0
	for ; i < len(s); i++ {
		switch c := s[i]; c {
		case '"', '\'', '`':
			j := i + 1
			for j < len(s) && s[j] != c {
				if s[j] == '\\' && c != '`' {
					j++
				}
				j++
			}
			i = j
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth == 0 {
				if c == '}' {
					return i
				}
				return -1
			}
			depth--
		}
	}
	return -1
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
		case h.at("{{"):
			h.i += 2
		case h.at("{/*"):
			end := strings.Index(h.src[h.i:], "*/}")
			if end < 0 {
				h.fail()
			}
			h.i += end + 3
		case h.holeAt():
			n, spread := h.hole()
			if spread {
				h.fail()
			}
			where := "a child"
			if parent != "" {
				where = "a child of <" + parent + ">"
			}
			h.add(slot{Label: n, Kind: kChild, Where: where})
		default:
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
		if h.holeAt() {
			n, spread := h.hole()
			if !spread {
				h.fail()
			}
			h.add(slot{Label: n, Kind: kSet, Where: "an attribute set of <" + tag + ">"})
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
		for h.i < end && tag != "script" && tag != "style" {
			switch {
			case h.at("{{"):
				h.i += 2
			case h.holeAt():
				n, _ := h.hole()
				h.add(slot{Label: n, Kind: kText, Where: "text in <" + tag + ">"})
			default:
				h.i++
			}
		}
		h.i = end
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
	if strings.HasPrefix(name, "class:") {
		s.Kind, s.Where = kToggle, name+" on <"+tag+">"
	}
	if h.holeAt() {
		s.Label, _ = h.hole()
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
	var found []int
	static := false
	for {
		if h.i >= len(h.src) {
			h.fail()
		}
		switch {
		case h.src[h.i] == q:
			h.i++
			if len(found) == 1 && !static {
				s.Label = found[0]
				h.add(s)
				return
			}
			if s.Kind != kToggle {
				s.Kind, s.Where = kPart, "part of "+s.Where
			}
			for _, n := range found {
				s.Label = n
				h.add(s)
			}
			return
		case h.at("{{"):
			static = true
			h.i += 2
		case h.holeAt():
			n, _ := h.hole()
			found = append(found, n)
		default:
			static = true
			h.i++
		}
	}
}
