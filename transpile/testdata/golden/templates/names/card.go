package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

// Card has fields named as the package's Title and Label.
type Card struct {
	Title string
	Items []string
}

// Label is the card's label.
func (c Card) Label() string { return "card" }

// Title is a package-level name the field wins over.
func Title() string { return "package" }

// Label is too.
var Label = "package"

func main() {
	out, err := ui.String(context.Background(), Card{Title: "field", Items: []string{"a"}})
	fmt.Println(out, err)
}
