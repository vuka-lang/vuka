package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

func main() {
	out, err := ui.String(context.Background(), ui.Component("s", nil, &Search{Q: "go", Accent: "#0a0"}))
	fmt.Println(out, err)
}
