package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

func main() {
	out, err := ui.String(context.Background(), (&Page{Q: "x"}).Render())
	fmt.Println(out, err)
}
