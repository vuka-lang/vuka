package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
	"vukatest.local/stateui/uigen"
)

type Row struct {
	Name string
}

func main() {
	b := &Board{}
	uigen.States(b) // what mounting a live component does
	b.Add("a")
	b.Add("b")
	b.Bump()
	b.Bump()
	b.Rename(0, "A")
	b.Reset()
	fmt.Println(b.rows.Get(), b.n.Get(), b.seen.Get())
	out, err := ui.String(context.Background(), b.Render())
	fmt.Println(out, err)
	fmt.Println(ui.Pushed())
}
