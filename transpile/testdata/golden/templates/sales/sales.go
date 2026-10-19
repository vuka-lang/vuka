package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

// Sales is a week of sales: its fields are its props.
type Sales struct {
	Title    string
	Week     []int
	Children ui.Node
}

// Total is the week's sum.
func (s Sales) Total() int {
	n := 0
	for _, d := range s.Week {
		n += d
	}
	return n
}

type Order struct {
	ID  int
	Ref string
}

func main() {
	out, err := ui.String(context.Background(), Page([]Order{{1, "A-1"}, {2, "B-2"}}))
	fmt.Println(out, err)
}
