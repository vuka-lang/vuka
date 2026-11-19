package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
)

func main() {
	d := &Dashboard{}
	out, err := ui.String(context.Background(), d.Render())
	fmt.Println(out, err)
	ui.LoadGroups(d, &d.Live)
	out, err = ui.String(context.Background(), d.Render())
	fmt.Println(out, err)
}
