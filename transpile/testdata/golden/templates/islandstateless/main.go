package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

func main() {
	out, err := ui.String(context.Background(), ui.IslandPage(&Page{Q: "x"}))
	fmt.Println(out, err)
}
