package main

import "github.com/vuka-lang/ui/v2"

// Item is a link.
type Item struct{ Href, Label string }

// NavLink is a function component.
func NavLink(item Item, on bool, label string) ui.Node { return ui.Text(label) }
