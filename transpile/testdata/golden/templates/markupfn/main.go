package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

type Order struct {
	Number  string
	Total   int
	Flagged bool
}

func main() {
	out, err := ui.String(context.Background(), Review{Title: "t", Submitted: []Order{{"A1", 1, false}}, Flagged: []Order{{"B2", 2, true}}})
	fmt.Println(out, err)
}
