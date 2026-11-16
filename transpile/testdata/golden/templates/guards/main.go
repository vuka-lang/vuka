package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
)

type Row struct {
	ID   int
	Name string
}

type Clock struct{ Zone string }

func Badge(label string) ui.Node { return ui.HTML(`<em>{label}</em>`, label) }

func main() {
	p := &Page{Title: "t", Limit: 3, Tags: []string{"x"}, Clock: &Clock{"utc"}}
	p.Add("a")
	p.Add("b")
	out, err := ui.String(context.Background(), p.Render())
	fmt.Println(out, err)
}
