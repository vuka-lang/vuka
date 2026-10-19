package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

// Chart is a component with a script: its data fields are its props.
type Chart struct {
	Title  string
	Points []int
	Zoom   func(from, to int)
}

// Toggle's script reads its state.
type Toggle struct {
	open ui.State[bool]
}

func main() {
	out, err := ui.String(context.Background(), Chart{Title: "Sales", Points: []int{3, 5}})
	fmt.Println(out, err)
	out, err = ui.String(context.Background(), &Toggle{open: ui.NewState("toggle", true)})
	fmt.Println(out, err)
}
