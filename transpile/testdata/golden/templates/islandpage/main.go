package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

func main() {
	out, err := ui.String(context.Background(), (&Page{Q: "x"}).Render())
	fmt.Println(out, err)
}
