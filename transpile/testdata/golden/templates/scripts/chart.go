package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
	"github.com/vuka-lang/ui/v2/uigen"
)

func main() {
	out, err := ui.String(context.Background(), Chart{Title: "Sales", Points: []int{3, 5}})
	fmt.Println(out, err)
	out, err = ui.String(context.Background(), &Toggle{open: uigen.State("toggle", true)})
	fmt.Println(out, err)
}
