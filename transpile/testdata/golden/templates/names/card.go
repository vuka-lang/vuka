package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

// Title is a package-level name the field wins over.
func Title() string { return "package" }

// Label is too.
var Label = "package"

func main() {
	out, err := ui.String(context.Background(), Card{Title: "field", Items: []string{"a"}})
	fmt.Println(out, err)
}
