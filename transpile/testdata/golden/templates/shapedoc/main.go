package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

func main() {
	c := &Counter{Start: 5}
	out, err := ui.String(context.Background(), ui.HTML(`{c}`, c))
	fmt.Println(out, err)
}
