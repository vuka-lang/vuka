package main

import (
	"strconv"

	"github.com/vuka-lang/ui/v2"
)

// Badge is a function component a slot's content renders.
func Badge(n int) ui.Node {
	return ui.El("span", []ui.Attr{{Name: "class", Value: "badge"}}, ui.Text(strconv.Itoa(n)))
}
