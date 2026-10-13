package transpile

import (
	"strings"
	"testing"
)

// probeDrift transpiles src and, for the first occurrence of needle in the
// source, maps that source offset to the generated file and back, reporting
// what the generated text looks like there.
func probeDrift(t *testing.T, src, needle string) {
	t.Helper()
	res, err := Package([]File{{Name: "p.vuka", Src: []byte(src)}}, Options{})
	if err != nil {
		t.Fatalf("transpile: %v", err)
	}
	out := res.Files[0]
	m := out.Map
	soff := strings.Index(src, needle)
	if soff < 0 {
		t.Fatalf("needle %q not in source", needle)
	}
	gen, exact := m.ToGenerated(soff)
	back, bexact, ok := m.ToSource(gen)
	gtxt := ""
	if gen >= 0 && gen < len(out.Src) {
		end := gen + 20
		if end > len(out.Src) {
			end = len(out.Src)
		}
		gtxt = string(out.Src[gen:end])
	}
	t.Logf("needle %q src=%d -> gen=%d (exact=%v) genText=%q ; back=%d (exact=%v ok=%v) want back=%d",
		needle, soff, gen, exact, gtxt, back, bexact, ok, soff)
	if ok && back != soff {
		t.Errorf("DRIFT: %q src=%d mapped to gen=%d which maps back to %d (genText=%q)", needle, soff, gen, back, gtxt)
	}
	if !strings.HasPrefix(strings.TrimLeft(gtxt, " \t"), needle) && !strings.HasPrefix(gtxt, needle[:1]) {
		t.Errorf("gen text at mapped offset is %q, expected to start near %q", gtxt, needle)
	}
}

func TestProbeDriftMatch(t *testing.T) {
	src := `package p

func use(x int) int {
	match x {
	case 1:
		return 10
	case _:
		return 20
	}
	return 0
}

func later(y int) int {
	return y + 1
}
`
	probeDrift(t, src, "y + 1")
	probeDrift(t, src, "return y")
}

func TestProbeDriftEnum(t *testing.T) {
	src := `package p

enum Status { Draft, Sent, Paid }

func later(y int) int {
	return y + 1
}
`
	probeDrift(t, src, "y + 1")
	probeDrift(t, src, "later(y int)")
}

func TestProbeDriftFieldAttr(t *testing.T) {
	src := `package p

type Char struct{ Max int }

type User struct {
	Name string @Char{Max: 50}
}

func later(y int) int {
	return y + 1
}
`
	probeDrift(t, src, "y + 1")
	probeDrift(t, src, "later(y int)")
}

func TestProbeDriftDecorator(t *testing.T) {
	src := `package p

import "github.com/vuka-lang/vuka"

func logit(c *vuka.Call) { c.Next() }

@logit
func work(n int) int {
	return n * 2
}

func later(y int) int {
	return y + 1
}
`
	probeDrift(t, src, "y + 1")
	probeDrift(t, src, "n * 2")
}
