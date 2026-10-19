package main

import "github.com/vuka-lang/ui"

// Card frames its children under a title, a footer in its slot; its
// script marks it seen, its style is its own (card.vuka.html).
type Card struct {
	Title    string
	Accent   string
	Wide     bool
	Footer   ui.Node
	Children ui.Node
}
