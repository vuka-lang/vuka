package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

func main() {
	out, err := ui.String(context.Background(), ui.IslandPage(&Page{Q: "x"}))
	fmt.Println(out, err)
}
