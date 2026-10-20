package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

// Glyph is a named shape.
type Glyph struct{ Name string }

// Path is the glyph's SVG path.
func (g Glyph) Path() string { return "M0 0L" + g.Name }

// Raw is a glyph not yet named one.
type Raw struct{ Name string }

// Icon has a field named after its own type: where a value goes, Glyph is
// the field; where only a type goes, the type.
type Icon struct {
	Glyph Glyph
	Alt   Raw
	Extra []Glyph
	Size  int
}

func main() {
	out, err := ui.String(context.Background(), Icon{Glyph: Glyph{Name: "1"}, Alt: Raw{Name: "2"}, Extra: []Glyph{{Name: "3"}}, Size: 16})
	fmt.Println(out, err)
}
