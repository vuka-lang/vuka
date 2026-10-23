package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

func main() {
	out, err := ui.String(context.Background(), Page{Tasks: []Task{{"Write", true}, {"Ship", false}}})
	fmt.Println(out, err)
}
