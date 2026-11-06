package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
)

func main() {
	out, err := ui.String(context.Background(), &Menu{})
	fmt.Println(out, err)
}
