package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

// AdminProps is the admin layout's: its title, Children and the Actions slot.
type AdminProps struct {
	Title    string
	Actions  ui.Node
	Children ui.Node
}

type userKey struct{}

func main() {
	ctx := context.WithValue(context.Background(), userKey{}, "ada")
	out, err := ui.String(ctx, Orders(3))
	fmt.Println(out, err)
}
