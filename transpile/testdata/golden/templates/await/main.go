package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
)

func main() {
	o := &Overview{}
	out, err := ui.String(context.Background(), o.Render())
	fmt.Println(out, err)
	ui.LoadGroups(o, &o.Live)
	out, err = ui.String(context.Background(), o.Render())
	fmt.Println(out, err)
}
