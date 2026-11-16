package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
)

func main() {
	l := &List{}
	l.Add("a")
	g := &Grid{}
	g.rows.Set([]string{"1", "2"})
	g.cols.Set([]string{"x"})
	for _, n := range []ui.Node{l.Render(), g.Render()} {
		out, err := ui.String(context.Background(), n)
		fmt.Println(out, err)
	}
}
