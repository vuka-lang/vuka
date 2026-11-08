package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
)

type Product struct {
	Name  string
	Price int
}

func main() {
	p := &Products{}
	fmt.Println(p.edit(context.Background()), p.Form.Value.Price)
	out, err := ui.String(context.Background(), p.Render())
	fmt.Println(out, err)
}
