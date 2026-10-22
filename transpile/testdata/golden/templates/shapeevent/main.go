package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui"
)

func main() {
	like, tag := &Like{}, Tag{Name: "go"}
	out, err := ui.String(context.Background(), ui.HTML(`{like}{tag}`, like, tag))
	fmt.Println(out, err)
}
