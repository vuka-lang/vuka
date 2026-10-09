package templx_test

import (
	"context"
	"io"
	"testing"

	"github.com/a-h/templ"
	"github.com/vuka-lang/vuka"
	"github.com/vuka-lang/vuka/templx"
)

// card is what templ generates for <div class="card">{ children... }</div>.
var card = templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
	io.WriteString(w, `<div class="card">`)
	if err := templ.GetChildren(ctx).Render(templ.ClearChildren(ctx), w); err != nil {
		return err
	}
	_, err := io.WriteString(w, `</div>`)
	return err
})

func TestWithChildren(t *testing.T) {
	tests := []struct {
		name string
		n    vuka.Node
		want string
	}{
		{"children", templx.WithChildren(card, vuka.Fragment(vuka.El("b", nil, vuka.Text("<hi>")), vuka.Text("!"))), `<div class="card"><b>&lt;hi&gt;</b>!</div>`},
		{"no children", templx.WithChildren(card, nil), `<div class="card"></div>`},
		{"nested", templx.WithChildren(card, templx.WithChildren(card, vuka.Text("in"))), `<div class="card"><div class="card">in</div></div>`},
		{"inside vuka", vuka.El("main", nil, templx.WithChildren(card, vuka.Text("x"))), `<main><div class="card">x</div></main>`},
	}
	for _, tt := range tests {
		got, err := vuka.String(context.Background(), tt.n)
		if err != nil || got != tt.want {
			t.Errorf("%s: %q, %v; want %q", tt.name, got, err, tt.want)
		}
	}
}

func TestInterchangeable(t *testing.T) {
	var c templ.Component = vuka.El("p", nil)
	var n vuka.Node = card
	_, _ = c, n
}
