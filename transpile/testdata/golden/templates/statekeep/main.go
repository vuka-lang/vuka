package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
	"vukatest.local/stateui/uigen"
)

func main() {
	_, err := ui.String(context.Background(), &Prefs{})
	fmt.Println(len(uigen.Notified), err)
}
