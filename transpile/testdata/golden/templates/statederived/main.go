package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
	"vukatest.local/stateui/uigen"
)

func main() {
	c := &Cart{}
	uigen.States(c)
	out, err := ui.String(context.Background(), c.Render())
	fmt.Println(out, err, c.total.Get(), c.over.Get())
}
