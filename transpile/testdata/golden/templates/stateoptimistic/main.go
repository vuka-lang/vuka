package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
	"vukatest.local/stateui/uigen"
)

func main() {
	p := &Post{}
	uigen.States(p)
	out, err := ui.String(context.Background(), p.Render())
	fmt.Println(out, err)
}
