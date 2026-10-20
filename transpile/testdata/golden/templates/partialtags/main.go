package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/vuka-lang/ui"
)

// Item is a link of the menu.
type Item struct{ Href, Label string }

type userKey struct{}

// badge is a function component written in Go: a count in a pill.
func badge(n int) ui.Node {
	return ui.HTML(`<b class="badge">{n}</b>`, n)
}

func main() {
	ctx := context.WithValue(context.Background(), userKey{}, "ada")
	out, err := ui.String(ctx, Menu([]Item{{"/", "Home"}, {"/orders", "Orders"}}, "/orders"))
	fmt.Println(strings.Join(strings.Fields(out), " "), err)
}
