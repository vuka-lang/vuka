package main

import (
	"context"
	"fmt"
	"slices"

	ui "vukatest.local/stateui"
	"vukatest.local/stateui/uigen"
)

// stash takes the state itself: Park's pass classifies draft synced.
func stash(s ui.State[string]) {}

func main() {
	o := &Orders{}
	uigen.States(o) // what mounting a live component does
	o.mount(context.Background())
	o.Add("tea")
	o.Add("beans")
	o.Show(1)
	fmt.Println(o.open.Get(), o.selected.Get().Name, o.tab.Get())
	o.Confirm()
	fmt.Println(o.open.Get(), o.selected.Get().Name)
	out, err := ui.String(context.Background(), o.Render())
	fmt.Println(out, err)
	synced := uigen.SyncedOf["Orders"]
	slices.Sort(synced)
	fmt.Println(synced) // note is browser-local: never listed
}
