package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

// Admin is the admin pages' layout: its Children and the Actions slot.
type Admin struct {
	Title    string
	Actions  ui.Node
	Children ui.Node
}

func main() {
	out, err := ui.String(context.Background(), Orders(3))
	fmt.Println(out, err)
}
