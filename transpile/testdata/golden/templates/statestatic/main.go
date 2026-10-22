package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
)

func main() {
	out, err := ui.String(context.Background(), &Menu{Label: "Menu"})
	fmt.Println(out, err)
}
