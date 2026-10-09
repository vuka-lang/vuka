package transpile

import (
	"bytes"
	"go/token"
	"sort"
	"strings"
)

// edit replaces src[start:end] with text.
type edit struct {
	start, end int
	text       string
	prio       int // orders insertions at the same offset: lower first
}

type edits []edit

func (es edits) sorted() edits {
	out := append(edits(nil), es...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.start != b.start {
			return a.start < b.start
		}
		if (a.start == a.end) != (b.start == b.end) {
			return a.start == a.end
		}
		return a.prio < b.prio
	})
	return out
}

// apply returns src with es (sorted, disjoint) applied. When an edit changes the
// length of a line that has more code after it, after(end) is written behind the
// replacement so the compiler's columns stay in step with the source.
func (es edits) apply(src []byte, after func(off int) string) []byte {
	var b bytes.Buffer
	last := 0
	for _, e := range es {
		b.Write(src[last:e.start])
		b.WriteString(e.text)
		last = e.end
		if after != nil && len(e.text) != e.end-e.start && restOfLineHasCode(src, e.end) {
			b.WriteString(after(e.end))
		}
	}
	b.Write(src[last:])
	return b.Bytes()
}

// toOrig maps an offset in the applied text back to src. An offset inside a
// replacement maps to the start of what it replaced.
func (es edits) toOrig(off int) int {
	delta := 0
	for _, e := range es {
		s := e.start + delta
		if off < s {
			break
		}
		if off < s+len(e.text) {
			return e.start
		}
		delta += len(e.text) - (e.end - e.start)
	}
	return off - delta
}

// fromOrig maps an offset in src to the applied text.
func (es edits) fromOrig(off int) int {
	delta := 0
	for _, e := range es {
		if off < e.start {
			break
		}
		if off < e.end {
			return e.start + delta
		}
		delta += len(e.text) - (e.end - e.start)
	}
	return off + delta
}

func restOfLineHasCode(src []byte, off int) bool {
	for ; off < len(src) && src[off] != '\n'; off++ {
		if c := src[off]; c != ' ' && c != '\t' && c != '\r' {
			return true
		}
	}
	return false
}

func lineStart(src []byte, off int) int {
	return bytes.LastIndexByte(src[:off], '\n') + 1
}

// lineIndex holds the offset of each line's first byte.
type lineIndex []int

func newLineIndex(src []byte) lineIndex {
	li := lineIndex{0}
	for i, c := range src {
		if c == '\n' {
			li = append(li, i+1)
		}
	}
	return li
}

func (li lineIndex) pos(file string, off int) token.Position {
	i := sort.Search(len(li), func(i int) bool { return li[i] > off }) - 1
	return token.Position{Filename: file, Offset: off, Line: i + 1, Column: off - li[i] + 1}
}

func lineDirective(p token.Position) string {
	return "/*line " + p.Filename + ":" + itoa(p.Line) + ":" + itoa(p.Column) + "*/"
}

func itoa(n int) string {
	var b [20]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			return string(b[i:])
		}
	}
}

func commentLines(lines []string) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if l = strings.TrimRight(l, " \t\r"); l == "" {
			out[i] = "//"
		} else {
			out[i] = "// " + l
		}
	}
	return strings.Join(out, "\n")
}
