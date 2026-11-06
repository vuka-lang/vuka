package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
	"vukatest.local/stateui/uigen"
)

type Order struct {
	ID     int64
	Number string
}

func main() {
	p := &Page{}
	uigen.States(p)
	p.rows.Insert(Order{1, "A-1"})
	out, err := ui.String(context.Background(), p.Render())
	fmt.Println(out, err)
}
