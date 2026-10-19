package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

type Page struct{ Zone string }

func main() {
	out, err := ui.String(context.Background(), Page{Zone: "EAT"}.Render())
	fmt.Println(out, err)
}
