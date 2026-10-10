package main

import "bytes"

// maxDiffEdits caps the line diff: past it the texts are one hunk around
// everything that differs.
const maxDiffEdits = 1000

// diffHunks are the stretches where cur differs from from, in order: a Myers
// line diff, each changed run trimmed to the bytes that differ.
func diffHunks(from, cur []byte) []hunk {
	a, b := bytes.SplitAfter(from, []byte("\n")), bytes.SplitAfter(cur, []byte("\n"))
	pre := 0
	for pre < len(a) && pre < len(b) && bytes.Equal(a[pre], b[pre]) {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && bytes.Equal(a[len(a)-1-suf], b[len(b)-1-suf]) {
		suf++
	}
	base := func(lines [][]byte) (n int) {
		for _, l := range lines[:pre] {
			n += len(l)
		}
		return n
	}
	fromBase, curBase := base(a), base(b)
	a, b = a[pre:len(a)-suf], b[pre:len(b)-suf]
	ids := map[string]int{}
	intern := func(lines [][]byte) []int {
		out := make([]int, len(lines))
		for i, l := range lines {
			id, ok := ids[string(l)]
			if !ok {
				id = len(ids)
				ids[string(l)] = id
			}
			out[i] = id
		}
		return out
	}
	pairs, ok := myers(intern(a), intern(b), maxDiffEdits)
	if !ok {
		return []hunk{spanHunk(from, cur)}
	}
	starts := func(lines [][]byte, base int) []int {
		out := make([]int, len(lines)+1)
		out[0] = base
		for i, l := range lines {
			out[i+1] = out[i] + len(l)
		}
		return out
	}
	as, bs := starts(a, fromBase), starts(b, curBase)
	var hunks []hunk
	add := func(x0, x1, y0, y1 int) {
		if x0 == x1 && y0 == y1 {
			return
		}
		h := hunk{bs[y0], bs[y1], as[x0], as[x1]}
		for h.cur < h.curEnd && h.from < h.fromEnd && cur[h.cur] == from[h.from] {
			h.cur, h.from = h.cur+1, h.from+1
		}
		for h.cur < h.curEnd && h.from < h.fromEnd && cur[h.curEnd-1] == from[h.fromEnd-1] {
			h.curEnd, h.fromEnd = h.curEnd-1, h.fromEnd-1
		}
		hunks = append(hunks, h)
	}
	x, y := 0, 0
	for _, p := range pairs {
		add(x, p[0], y, p[1])
		x, y = p[0]+1, p[1]+1
	}
	add(x, len(a), y, len(b))
	return hunks
}

// spanHunk is the one hunk around everything that differs between from and cur.
func spanHunk(from, cur []byte) hunk {
	pre := 0
	for pre < len(cur) && pre < len(from) && cur[pre] == from[pre] {
		pre++
	}
	suf := 0
	for suf < len(cur)-pre && suf < len(from)-pre && cur[len(cur)-1-suf] == from[len(from)-1-suf] {
		suf++
	}
	return hunk{pre, len(cur) - suf, pre, len(from) - suf}
}

// myers is the longest common subsequence of a and b as matched index pairs,
// in order; false when they differ by more than maxD insertions and deletions.
func myers(a, b []int, maxD int) ([][2]int, bool) {
	n, m := len(a), len(b)
	off := maxD + 1
	v := make([]int, 2*off+1)
	var trace [][]int // trace[d]: v over k in [-d, d] before step d
	for d := 0; d <= maxD; d++ {
		trace = append(trace, append([]int(nil), v[off-d:off+d+1]...))
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || k != d && v[off+k-1] < v[off+k+1] {
				x = v[off+k+1]
			} else {
				x = v[off+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x, y = x+1, y+1
			}
			v[off+k] = x
			if x >= n && y >= m {
				return backtrack(trace, n, m), true
			}
		}
	}
	return nil, false
}

func backtrack(trace [][]int, x, y int) [][2]int {
	var pairs [][2]int
	snake := func(toX int) {
		for x > toX {
			x, y = x-1, y-1
			pairs = append(pairs, [2]int{x, y})
		}
	}
	for d := len(trace) - 1; d > 0; d-- {
		s := trace[d]
		at := func(k int) int { return s[k+d] }
		k := x - y
		pk := k - 1
		if k == -d || k != d && at(k-1) < at(k+1) {
			pk = k + 1
		}
		px := at(pk)
		mx := px
		if pk == k-1 {
			mx++
		}
		snake(mx)
		x, y = px, px-pk
	}
	snake(0)
	for i, j := 0, len(pairs)-1; i < j; i, j = i+1, j-1 {
		pairs[i], pairs[j] = pairs[j], pairs[i]
	}
	return pairs
}
