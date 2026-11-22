package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/live"
	"github.com/vuka-lang/ui/v2/live/livetest"
)

type Item struct {
	ID   int
	Name string
}

func main() {
	c, err := livetest.Join(live.NewSession(context.Background(), func(context.Context) ui.Node { return ui.HTML(`{b}`, &Board{}) }), 2)
	if err != nil {
		fmt.Println(err)
		return
	}
	show := func(when string) {
		fmt.Printf("%s: %q %q | %q %q\n", when, c.Text("#in-2 .count"), c.Text("#in-3 .count"), c.Text("#fn-2 .count"), c.Text("#fn-3 .count"))
	}
	for _, sel := range []string{"#in-2 .inc", "#in-2 .inc", "#fn-2 .inc", "#fn-2 .inc", "#fn-3 .inc"} {
		if err := c.Click(sel); err != nil {
			fmt.Println(sel, err)
		}
	}
	show("before")
	if err := c.Click("#drop"); err != nil {
		fmt.Println("drop", err)
	}
	show("after")
}
