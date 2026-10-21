package main

import (
	"context"
	"fmt"
	"github.com/vuka-lang/ui"
)

type userKey struct{}
type Page struct {
	ui.Live
	Title string
}
type Card struct{ Title string }

func main() {
	node := (&Page{Title: "Dashboard"}).Render()
	for _, name := range []string{"alice", "<bob>"} {
		ctx := context.WithValue(context.Background(), userKey{}, name)
		out, err := ui.String(ctx, node)
		fmt.Println(out, err)
	}
}
