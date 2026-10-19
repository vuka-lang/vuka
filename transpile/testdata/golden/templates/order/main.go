package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

func main() {
	out, err := ui.String(context.Background(), Page([]int{1, 2, 3}))
	fmt.Println(out, err)
}

type counter struct{ n int }

func (c *counter) next() int { c.n++; return c.n }
