package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/live"
	"github.com/vuka-lang/ui/v2/live/livetest"
)

type Order struct {
	ID       int
	Number   string
	Approved bool
}

// A click inside a markup func's body pushes the page's method, and its
// reply is no bigger than the same click's in a row written inline: the
// body diffs per call as a loop's body does.
func main() {
	c, err := livetest.Join(live.NewSession(context.Background(), func(context.Context) ui.Node {
		b := &Board{}
		b.Load()
		return ui.HTML(`{b}`, b)
	}), 2)
	if err != nil {
		fmt.Println(err)
		return
	}
	click := func(sel string) int {
		before := c.Bytes
		if err := c.Click(sel); err != nil {
			fmt.Println(sel, err)
		}
		return c.Bytes - before
	}
	in, fn := click("#in-3 .ok"), click("#fn-3 .ok")
	fmt.Println(c.Text("#in-3"), "|", c.Text("#fn-3"), "|", c.HasClass("#fn-3", "done"), c.HasClass("#fn-2", "done"))
	fmt.Println("first replies, statics and all: no bigger:", fn <= in, in > 0)
	in, fn = click("#in-4 .ok"), click("#fn-4 .ok")
	fmt.Println(c.Text("#fn-4"), "| again equal:", in == fn)
}
