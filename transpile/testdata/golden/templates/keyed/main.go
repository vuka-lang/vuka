package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

type Item struct {
	ID   int
	Name string
}

// Counter is a stateful component, keyed in a loop.
type Counter struct {
	ui.Live
	Label string
	n     int
}

func (c *Counter) Render() ui.Node {
	return ui.HTML(`<span>{c.Label}: {c.n}</span>`, c.Label, c.n)
}

// List's markup is list.vuka.html.
type List struct{ Items []Item }

func main() {
	out, err := ui.String(context.Background(), List{Items: []Item{{1, "a"}, {2, "b"}}})
	fmt.Println(out, err)
}
