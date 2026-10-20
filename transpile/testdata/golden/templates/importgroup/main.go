package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

func main() {
	out, err := ui.String(context.Background(), Total([]int{3, 4}))
	fmt.Println(out, err)
}
