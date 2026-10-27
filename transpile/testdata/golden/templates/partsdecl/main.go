package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
)

type Service struct{}

func main() {
	out, err := ui.String(context.Background(), &Page{})
	fmt.Println(out, err)
}
