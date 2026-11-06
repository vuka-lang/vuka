package main

import (
	"github.com/a-h/templ"
	"github.com/vuka-lang/ui/v2"
)

func main() {}

// Box is a templ component with a footer.
func Box(title string, footer templ.Component) templ.Component { return templ.NopComponent }

// Panel is a function component with an actions slot.
func Panel(actions ui.Node, children ui.Node) ui.Node { return children }
