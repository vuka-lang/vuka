package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
)

func main() {
	out, err := ui.String(context.Background(), Orders{Title: "Orders", Rows: []Row{{"pens", 2}, {"ink", 1}}})
	fmt.Println(out, err)
}
