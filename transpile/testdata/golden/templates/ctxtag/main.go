package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

type userKey struct{}

func main() {
	ctx := context.WithValue(context.Background(), userKey{}, "ada")
	out, err := ui.String(ctx, Orders{N: 3})
	fmt.Println(out, err)
}
