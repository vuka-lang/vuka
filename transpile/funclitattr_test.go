package transpile_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/transpile"
)

const callbackShape = `<script lang="go">
package main

import (
	"context"

	ui "vukatest.local/opttarget"
)

type Order struct {
	ID     int
	Number string
}

type Review struct {
	ui.Live
	x int
}

func f(ctx context.Context) (int, error) { return 1, nil }
</script>

`

const callbackActs = `package main

import (
	"context"
	"io"

	ui "vukatest.local/opttarget"
)

type Acts struct {
	OnActed func(ctx context.Context) error
	Pick    func(Order)
}

func (a Acts) View() ui.Node                                    { return nil }
func (a Acts) Render(ctx context.Context, w io.Writer) error { return nil }
`

// TestFuncLitAttrSyntax: a stray bracket after, or inside, a func literal
// set on an attribute is reported at the bracket in Go's terms, not as an
// unclosed { at the attribute's start.
func TestFuncLitAttrSyntax(t *testing.T) {
	// The markup starts on line 23.
	for _, c := range []struct{ name, markup, pos, msg string }{
		{"after the literal", "<Acts onActed={func(ctx context.Context) error { x = f(ctx)?; return nil })}/>\n",
			"23:75", "unexpected ) after the func literal; the attribute closes with }"},
		{"spaced", "<Acts onActed={func(ctx context.Context) error { return nil } )}/>\n",
			"23:63", "unexpected ) after the func literal"},
		{"in the body", "<Acts onActed={func(ctx context.Context) error {\n  x = f(ctx)?\n  return nil )\n}}/>\n",
			"25:14", "unexpected ) in the func literal's body"},
		{"on a Go prop", "<Acts pick={func(o Order) { println(o.Number) })}/>\n",
			"23:48", "unexpected ) after the func literal; the attribute closes with }"},
		{"on an element", "<button onClick={func() { println(1) })}>x</button>\n",
			"23:39", "unexpected ) after the func literal"},
		{"in a hole", "<p>{len(\"ab\"))}</p>\n",
			"23:14", "unexpected ): nothing it closes is open; the { at line 23 closes with }"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := compileShapes(map[string]string{"review.vuka.html": callbackShape + c.markup, "acts.go": callbackActs})
			var list transpile.ErrorList
			if !errors.As(err, &list) || len(list) != 1 {
				t.Fatalf("want one error, got %v", err)
			}
			e := list[0]
			if e.Code != "E0003" {
				t.Errorf("code %s, want E0003", e.Code)
			}
			if got := strings.TrimPrefix(e.Pos.String(), "review.vuka.html:"); got != c.pos || !strings.Contains(e.Msg, c.msg) {
				t.Errorf("%s: %s, want %s: %s", got, e.Msg, c.pos, c.msg)
			}
		})
	}
	// The literal well formed, ? and all, compiles.
	if _, err := compileShapes(map[string]string{"review.vuka.html": callbackShape + "<Acts onActed={func(ctx context.Context) error {\n  x = f(ctx)?\n  return nil\n}}/>\n", "acts.go": callbackActs}); err != nil {
		t.Fatal(err)
	}
}
