package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

func main() {
	ctx := context.Background()
	for _, n := range []ui.Node{
		Frame{},
		Frame{Actions: ui.HTML(`<button>Save</button>`), Footer: ui.Text("  ")},
		Frame{Footer: ui.Text("fine print")},
	} {
		out, err := ui.String(ctx, n)
		fmt.Println(out, err)
	}
}
