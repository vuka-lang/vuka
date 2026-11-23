package main

import (
	"context"
	"fmt"
	"slices"

	ui "vukatest.local/stateui"
	"vukatest.local/stateui/uigen"
)

func main() {
	o := &Orders{}
	uigen.States(o) // what mounting a live component does
	o.Load()
	out, err := ui.String(context.Background(), o.Render())
	fmt.Println(out, err)
	synced := uigen.SyncedOf["Orders"]
	slices.Sort(synced)
	fmt.Println(synced) // note and tab are browser-local: never listed
}
