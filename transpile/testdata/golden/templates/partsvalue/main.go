package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
)

func main() {
	out, err := ui.String(context.Background(), &Page{N: 4})
	fmt.Println(out, err)
	out, err = ui.String(context.Background(), Card{N: 2})
	fmt.Println(out, err)
}
