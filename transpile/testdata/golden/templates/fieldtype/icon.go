package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

// Glyph is a named shape.
type Glyph struct{ Name string }

// Path is the glyph's SVG path.
func (g Glyph) Path() string { return "M0 0L" + g.Name }

// Raw is a glyph not yet named one.
type Raw struct{ Name string }

func main() {
	out, err := ui.String(context.Background(), Icon{Glyph: Glyph{Name: "1"}, Alt: Raw{Name: "2"}, Extra: []Glyph{{Name: "3"}}, Size: 16})
	fmt.Println(out, err)
}
