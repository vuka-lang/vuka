package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
)

func main() {
	out, err := ui.String(context.Background(), Pets{Names: []string{"rex", "tom"}})
	fmt.Println(out, err)
}
