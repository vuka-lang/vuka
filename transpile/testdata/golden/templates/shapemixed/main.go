package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

type Pet struct {
	Name string
	Age  int
}

func main() {
	out, err := ui.String(context.Background(), Pets{Pets: []Pet{{"Rex", 3}, {"Tom", 5}}})
	fmt.Println(out, err)
}
