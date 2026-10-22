package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

func main() {
	like, tag := &Like{}, Tag{Name: "go"}
	out, err := ui.String(context.Background(), ui.HTML(`{like}{tag}`, like, tag))
	fmt.Println(out, err)
}
