package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

// Search binds its query to its input, and colours itself with a CSS
// variable.
type Search struct {
	ui.Live
	Q      string
	Accent string
}

func (s *Search) LiveSchema(l *ui.LiveSchema) { l.Field(&s.Q, ui.Bind{}) }

func main() {
	out, err := ui.String(context.Background(), ui.Component("s", nil, &Search{Q: "go", Accent: "#0a0"}))
	fmt.Println(out, err)
}
