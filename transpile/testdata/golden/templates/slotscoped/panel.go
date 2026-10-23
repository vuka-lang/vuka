package main

import "github.com/vuka-lang/ui/v2"

// Panel is a function component whose row parameter is a scoped slot.
func Panel(title string, n int, row func(int) ui.Node) ui.Node {
	var rows []ui.Node
	for i := range n {
		rows = append(rows, row(i))
	}
	return ui.El("section", nil, ui.El("h3", nil, ui.Text(title)), ui.Fragment(rows...))
}
