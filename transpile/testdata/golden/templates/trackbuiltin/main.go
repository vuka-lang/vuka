package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
	"vukatest.local/stateui/uigen"
)

// Meta is a tracked struct holding a map.
type Meta struct{ Tags map[string]bool }

func main() {
	c := &Cart{}
	uigen.States(c)
	c.meta.Set(Meta{Tags: map[string]bool{"new": true}})
	c.Add("pen", 2)
	c.Add("ink", 1)
	c.Fill([]string{"a", "b"})
	show := func() {
		out, _ := ui.String(context.Background(), c.Render())
		fmt.Println(c.items.Get(), c.items.Writes(), c.slots.Get(), c.slots.Writes(), out)
	}
	show()
	c.Remove("pen")
	show()
	c.Empty()
	show()
	c.Fill([]string{"z"})
	show()
	c.Untag("new")
	fmt.Println(c.meta.Get(), c.meta.Writes())
}
