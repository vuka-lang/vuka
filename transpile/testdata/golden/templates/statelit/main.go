package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
	"vukatest.local/stateui/uigen"
)

func main() {
	bs := newBadges()
	bs = append(bs, bs[1].twin())
	for _, b := range bs {
		uigen.States(b)
		out, err := ui.String(context.Background(), b.Render())
		fmt.Println(out, err)
	}
	z := zero()
	fmt.Println(z.count.Get())
	p := pill()
	fmt.Println(p.Label, p.n.Get())
}
