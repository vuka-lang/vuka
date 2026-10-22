package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

type Item struct {
	ID   int
	Name string
}

func main() {
	out, err := ui.String(context.Background(), List{Items: []Item{{1, "a"}, {2, "b"}}})
	fmt.Println(out, err)
}
