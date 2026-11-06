package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
	"vukatest.local/stateui/uigen"
)

func main() {
	s := &Search{}
	uigen.States(s)
	_, err := ui.String(context.Background(), s.Render())
	fmt.Println(len(uigen.Notified), err)
}
