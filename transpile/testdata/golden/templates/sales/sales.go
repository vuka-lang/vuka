package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

type Order struct {
	ID  int
	Ref string
}

func main() {
	out, err := ui.String(context.Background(), Page{Orders: []Order{{1, "A-1"}, {2, "B-2"}}})
	fmt.Println(out, err)
}
