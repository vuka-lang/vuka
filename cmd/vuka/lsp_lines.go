package main

import (
	"sort"
	"unicode/utf8"
)

// lineIndex is where each line of a buffer starts: it maps LSP positions
// and byte offsets in the time of a binary search and a walk of one line,
// where offsetOf and positionOf read the buffer from its start.
type lineIndex struct {
	buf    []byte
	starts []int // the offset of each line's first byte
}

func newLineIndex(buf []byte) *lineIndex {
	starts := []int{0}
	for i, b := range buf {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return &lineIndex{buf: buf, starts: starts}
}

// offset is offsetOf(x.buf, pos).
func (x *lineIndex) offset(pos lspPosition) int {
	if int(pos.Line) >= len(x.starts) {
		return len(x.buf)
	}
	off := x.starts[pos.Line]
	units := uint32(0)
	for off < len(x.buf) && units < pos.Character && x.buf[off] != '\n' {
		r, size := utf8.DecodeRune(x.buf[off:])
		units += uint32(runeLen16(r))
		off += size
	}
	return off
}

// position is positionOf(x.buf, off).
func (x *lineIndex) position(off int) lspPosition {
	off = min(max(off, 0), len(x.buf))
	line := sort.SearchInts(x.starts, off+1) - 1
	units := 0
	for _, r := range string(x.buf[x.starts[line]:off]) {
		units += runeLen16(r)
	}
	return lspPosition{Line: uint32(line), Character: uint32(units)}
}

// lineStart is lineStart(x.buf, line).
func (x *lineIndex) lineStart(line uint32) int { return x.offset(lspPosition{Line: line}) }

// index keeps the line starts of f's texts, for the mappings that run once
// per token or diagnostic. Set before f is shared, and by withText.
func (f *vfile) index() {
	f.genLines, f.fromLines, f.curLines = newLineIndex(f.gen), newLineIndex(f.from), nil
	if f.cur != nil {
		f.curLines = newLineIndex(f.cur)
	}
}

// lines is the line index of buf, one of f's texts: the one f keeps when
// it has it, else one made now.
func (f *vfile) lines(buf []byte) *lineIndex {
	for _, x := range [...]*lineIndex{f.curLines, f.fromLines, f.genLines} {
		if x != nil && sameBytes(x.buf, buf) {
			return x
		}
	}
	return newLineIndex(buf)
}

// textLines is the line index of f.text().
func (f *vfile) textLines() *lineIndex { return f.lines(f.text()) }

// sameBytes reports whether a and b are the same slice of the same array.
func sameBytes(a, b []byte) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}
