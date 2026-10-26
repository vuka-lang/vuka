package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

func main() {
	for _, n := range []ui.Node{
		Page{Rows: []Row{{"a", 1}, {"b", 2}}},
		Page{},
		Panel{Title: "Recent", Rows: []Row{{"c", 3}}},
		Panel{Title: "Empty"},
		Styled{Rows: []Row{{"d", 4}}},
	} {
		out, err := ui.String(context.Background(), n)
		fmt.Println(out, err)
	}
}
