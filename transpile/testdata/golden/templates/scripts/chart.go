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

func main() {
	out, err := ui.String(context.Background(), Chart{Title: "Sales", Points: []int{3, 5}}.Render())
	fmt.Println(out, err)
}
