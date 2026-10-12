package format

import (
	"go/token"
	"strings"
)

// enumAt reads the enum declaration whose keyword is toks[i] and lays it out:
// one line as written when it is on one line, `enum Status { Draft, Sent }`;
// otherwise a variant a line, indented by depth, values' = aligned in runs.
// It returns the index of its closing brace.
func enumAt(src []byte, toks []tok, i int) (*construct, int, error) {
	j := i + 2
	head := "enum " + toks[i+1].lit
	if j < len(toks) && toks[j].tok == token.IDENT {
		head += " " + toks[j].lit
		j++
	}
	if j >= len(toks) || toks[j].tok != token.LBRACE {
		return nil, 0, errorAt(src, toks[i].off, "expected enum "+toks[i+1].lit+" { Variant, … }")
	}
	end, _, ok := matchClose(toks, j)
	if !ok {
		return nil, 0, errorAt(src, toks[i].off, "unclosed enum "+toks[i+1].lit)
	}
	last := end - 1
	body := string(src[toks[j].off+1 : toks[last].off])
	c := &construct{kind: 'N', start: toks[i].off, end: toks[last].end()}
	if !strings.Contains(body, "\n") {
		var parts []string
		for _, p := range splitTop(body) {
			if p = strings.Join(strings.Fields(p), " "); p != "" {
				parts = append(parts, p)
			}
		}
		c.text = head + " { " + strings.Join(parts, ", ") + " }"
		return c, last, nil
	}
	var lines []string
	depth, blank := 1, false
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			blank = len(lines) > 0
			continue
		}
		d := depth
		if strings.HasPrefix(line, "}") {
			d--
		}
		if blank {
			lines = append(lines, "")
			blank = false
		}
		lines = append(lines, strings.Repeat("\t", max(d, 1))+line)
		for _, t := range scanTokens([]byte(line), 0) {
			switch {
			case t.tok == token.LBRACE:
				depth++
			case t.tok == token.RBRACE:
				depth--
			}
		}
	}
	alignValues(lines)
	c.text = head + " {\n" + strings.Join(lines, "\n") + "\n}"
	return c, last, nil
}

// splitTop splits s at its commas outside brackets.
func splitTop(s string) []string {
	var out []string
	from, depth := 0, 0
	for _, t := range scanTokens([]byte(s), 0) {
		switch {
		case isOpen(t.tok):
			depth++
		case isClose(t.tok):
			depth--
		case t.tok == token.COMMA && depth == 0:
			out = append(out, s[from:t.off])
			from = t.end()
		}
	}
	return append(out, s[from:])
}

// alignValues aligns the = of consecutive `Name = value` lines, as gofmt
// aligns a const block.
func alignValues(lines []string) {
	run := []int{}
	flush := func() {
		width := 0
		for _, i := range run {
			name, _, _ := strings.Cut(strings.TrimLeft(lines[i], "\t"), "=")
			width = max(width, len(strings.TrimSpace(name)))
		}
		for _, i := range run {
			indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], "\t"))]
			name, value, _ := strings.Cut(strings.TrimLeft(lines[i], "\t"), "=")
			name = strings.TrimSpace(name)
			lines[i] = indent + name + strings.Repeat(" ", width-len(name)) + " = " + strings.TrimSpace(value)
		}
		run = run[:0]
	}
	for i, l := range lines {
		ts := scanTokens([]byte(l), 0)
		if strings.Count(l, "\t")-strings.Count(strings.TrimLeft(l, "\t"), "\t") == 1 && len(ts) > 1 && ts[0].tok == token.IDENT && ts[1].tok == token.ASSIGN {
			run = append(run, i)
			continue
		}
		flush()
	}
	flush()
}
