package main

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// checkHunks fails unless hunks are ordered, apart, and turn from into cur.
func checkHunks(t *testing.T, from, cur string, hunks []hunk) {
	t.Helper()
	var b strings.Builder
	f, c := 0, 0
	for _, h := range hunks {
		if h.from < f || h.cur < c || h.fromEnd < h.from || h.curEnd < h.cur || h.from-f != h.cur-c {
			t.Fatalf("bad hunk %+v after from %d cur %d: %+v", h, f, c, hunks)
		}
		if from[f:h.from] != cur[c:h.cur] {
			t.Fatalf("unchanged stretch differs before %+v", h)
		}
		b.WriteString(from[f:h.from])
		b.WriteString(cur[h.cur:h.curEnd])
		f, c = h.fromEnd, h.curEnd
	}
	b.WriteString(from[f:])
	if b.String() != cur || len(from)-f != len(cur)-c {
		t.Fatalf("hunks %+v turn %q into %q, want %q", hunks, from, b.String(), cur)
	}
}

func TestDiffHunks(t *testing.T) {
	lines := func(n int, f func(i int) string) string {
		var b strings.Builder
		for i := range n {
			b.WriteString(f(i) + "\n")
		}
		return b.String()
	}
	base := lines(60, func(i int) string { return fmt.Sprintf("\tline%d := %d", i, i) })
	for _, tc := range []struct {
		name, cur string
		hunks     int
	}{
		{"identical", base, 0},
		{"edit at start", "//" + base, 1},
		{"edit at end", base + "}\n", 1},
		{"line added on top, tag broken below",
			strings.Replace(strings.Replace(base, "\tline5 := 5\n", "\tline5 := 5\n\tadded := 1\n", 1), "line40 := 40", "line40 := <h3>", 1), 2},
		{"lines deleted and changed apart",
			strings.Replace(strings.Replace(strings.Replace(base, "\tline2 := 2\n\tline3 := 3\n", "", 1), "line30 :=", "lineX :=", 1), "line50 := 50", "line50 := 51", 1), 3},
		{"emptied", "", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hs := diffHunks([]byte(base), []byte(tc.cur))
			checkHunks(t, base, tc.cur, hs)
			if len(hs) != tc.hunks {
				t.Fatalf("%d hunks, want %d: %+v", len(hs), tc.hunks, hs)
			}
		})
	}

	t.Run("offsets between and after edits", func(t *testing.T) {
		cur := strings.Replace(strings.Replace(base, "\tline5 := 5\n", "\tline5 := 5\n\tadded := 1\n", 1), "line40 := 40", "line40 := <h3>", 1)
		f := &vfile{from: []byte(base)}
		f.withText([]byte(cur), nil)
		for _, sym := range []string{"line3 ", "line20 ", "line40 ", "line55 "} {
			c, o := strings.Index(cur, sym), strings.Index(base, sym)
			if got := f.fromOff(c + 2); got != o+2 {
				t.Errorf("%s: fromOff %d, want %d", sym, got, o+2)
			}
			if got := f.curOff(o + 2); got != c+2 {
				t.Errorf("%s: curOff %d, want %d", sym, got, c+2)
			}
			if f.changedAt(c+2) || f.goneAt(o+2) {
				t.Errorf("%s counts as changed", sym)
			}
		}
		if at := strings.Index(cur, "added"); !f.changedAt(at + 1) {
			t.Error("the added line isn't changed")
		}
		if at := strings.Index(base, "line40 := 40") + len("line40 := 4"); !f.goneAt(at) {
			t.Error("the replaced value isn't gone")
		}
	})

	t.Run("random edits", func(t *testing.T) {
		r := rand.New(rand.NewSource(1))
		words := []string{"a\n", "b\n", "c\n", "if x {\n", "}\n", "\n", "return y\n"}
		for range 500 {
			var from, cur strings.Builder
			for range r.Intn(40) {
				w := words[r.Intn(len(words))]
				from.WriteString(w)
				switch r.Intn(6) {
				case 0:
				case 1:
					cur.WriteString(words[r.Intn(len(words))] + w)
				case 2:
					cur.WriteString(strings.TrimSuffix(w, "\n") + "z\n")
				default:
					cur.WriteString(w)
				}
			}
			checkHunks(t, from.String(), cur.String(), diffHunks([]byte(from.String()), []byte(cur.String())))
		}
	})

	t.Run("too many edits fall back to one hunk", func(t *testing.T) {
		from := lines(3000, func(i int) string { return fmt.Sprint("a", i) })
		cur := lines(3000, func(i int) string { return fmt.Sprint("b", i) })
		hs := diffHunks([]byte(from), []byte(cur))
		checkHunks(t, from, cur, hs)
		if len(hs) != 1 {
			t.Fatalf("%d hunks", len(hs))
		}
	})
}
