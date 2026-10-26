package main

import "github.com/vuka-lang/ui/v2"

// Column is one of a Table's columns: its key and label, and its cell.
type Column[T any] struct {
	Key   string
	Label string
	Cell  func(T) ui.Node
}
