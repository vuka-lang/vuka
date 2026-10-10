package transpile

import (
	"bytes"
	"go/token"
	"sort"
	"strconv"
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
	out, _ := es.applyMap(src, after)
	return out
}

// applyMap is apply that also records where each piece of the output came from.
func (es edits) applyMap(src []byte, after func(off int) string) ([]byte, []segment) {
	var b bytes.Buffer
	var segs []segment
	last := 0
	copySrc := func(from, to int) {
		if to > from {
			segs = append(segs, segment{gen: b.Len(), src: from, genLen: to - from, srcLen: to - from, copy: true})
			b.Write(src[from:to])
		}
	}
	for _, e := range es {
		copySrc(last, e.start)
		segs = append(segs, segment{gen: b.Len(), src: e.start, genLen: len(e.text), srcLen: e.end - e.start})
		b.WriteString(e.text)
		last = e.end
		if after != nil && len(e.text) != e.end-e.start && restOfLineHasCode(src, e.end) {
			d := after(e.end)
			segs = append(segs, segment{gen: b.Len(), src: e.end, genLen: len(d)})
			b.WriteString(d)
		}
	}
	copySrc(last, len(src))
	return b.Bytes(), segs
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

func itoa(n int) string { return strconv.Itoa(n) }

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

// segment is a piece of generated text and the source it came from: copied
// verbatim, or written in place of src[src:src+srcLen].
type segment struct {
	gen, src       int
	genLen, srcLen int
	copy           bool
}

// SourceMap relates offsets in a generated Go file to offsets in its .vuka
// source. Text Vuka copied maps exactly; text it wrote maps to the start of what
// it replaced.
type SourceMap struct {
	segs []segment // ordered by gen
}

func (m *SourceMap) add(base int, segs []segment) {
	for _, s := range segs {
		s.gen += base
		m.segs = append(m.segs, s)
	}
}

// ToSource maps a generated offset to the source. exact is false where Vuka
// wrote the text; ok is false where the text has no source at all.
func (m *SourceMap) ToSource(gen int) (src int, exact, ok bool) {
	i := sort.Search(len(m.segs), func(i int) bool { return m.segs[i].gen+m.segs[i].genLen > gen })
	if i == len(m.segs) {
		if n := len(m.segs); n > 0 && gen == m.segs[n-1].gen+m.segs[n-1].genLen && m.segs[n-1].copy {
			s := m.segs[n-1]
			return s.src + s.srcLen, true, true
		}
		return 0, false, false
	}
	s := m.segs[i]
	if gen < s.gen {
		return 0, false, false
	}
	if s.copy {
		return s.src + gen - s.gen, true, true
	}
	return s.src, false, s.srcLen > 0 || s.genLen > 0
}

// ToGenerated maps a source offset to the generated file. exact is false when
// the offset is inside text Vuka rewrote.
func (m *SourceMap) ToGenerated(src int) (gen int, exact bool) {
	best := -1
	for i, s := range m.segs {
		if s.copy && src >= s.src && src <= s.src+s.srcLen {
			return s.gen + src - s.src, true
		}
		if !s.copy && s.srcLen > 0 && src >= s.src && src < s.src+s.srcLen && best < 0 {
			best = i
		}
	}
	if best >= 0 {
		return m.segs[best].gen, false
	}
	return 0, false
}

// genWriter builds generated text along with the source each piece maps to.
type genWriter struct {
	b    strings.Builder
	segs []segment
}

func (w *genWriter) len() int       { return w.b.Len() }
func (w *genWriter) String() string { return w.b.String() }

// gen writes text Vuka made up, attributed to src.
func (w *genWriter) gen(text string, src int) {
	if text == "" {
		return
	}
	w.segs = append(w.segs, segment{gen: w.b.Len(), src: src, genLen: len(text)})
	w.b.WriteString(text)
}

// replace writes text Vuka wrote in place of src[src:src+n].
func (w *genWriter) replace(text string, src, n int) {
	if text == "" && n == 0 {
		return
	}
	w.segs = append(w.segs, segment{gen: w.b.Len(), src: src, genLen: len(text), srcLen: n})
	w.b.WriteString(text)
}

// copy writes text taken verbatim from the source at src.
func (w *genWriter) copy(text string, src int) {
	if text == "" {
		return
	}
	w.segs = append(w.segs, segment{gen: w.b.Len(), src: src, genLen: len(text), srcLen: len(text), copy: true})
	w.b.WriteString(text)
}

func (w *genWriter) append(o *genWriter) {
	base := w.b.Len()
	for _, s := range o.segs {
		s.gen += base
		w.segs = append(w.segs, s)
	}
	w.b.WriteString(o.b.String())
}

// Piece is a stretch of generated text and the source it came from.
type Piece struct {
	Gen, GenLen int // in the generated file
	Src, SrcLen int // in the .vuka file
	Copied      bool
}

// Pieces lists the map's stretches in generated order.
func (m *SourceMap) Pieces() []Piece {
	out := make([]Piece, len(m.segs))
	for i, s := range m.segs {
		out[i] = Piece{Gen: s.gen, GenLen: s.genLen, Src: s.src, SrcLen: s.srcLen, Copied: s.copy}
	}
	return out
}
