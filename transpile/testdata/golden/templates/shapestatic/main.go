package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

func main() {
	out, err := ui.String(context.Background(), Card{Title: "Pets", Tags: []string{"cat", "dog"}, Children: ui.Text("two")})
	fmt.Println(out, err)
}
