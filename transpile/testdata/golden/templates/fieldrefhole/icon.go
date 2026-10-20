package main

import "fmt"

// Glyph is a named shape.
type Glyph struct{ Name string }

// Icon reads a field reference, Glyph.Name, as a hole of its own.
type Icon struct{ Size int }

func main() { fmt.Println(Icon{}) }
