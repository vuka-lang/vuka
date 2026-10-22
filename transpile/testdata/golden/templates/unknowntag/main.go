package main

import "github.com/vuka-lang/ui/v2"

// Item is a link.
type Item struct{ Href, Label string }

// navLink is a function component.
func navLink(item Item) ui.Node { return ui.Text(item.Label) }
