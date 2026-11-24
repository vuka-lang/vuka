package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

func main() {
	out, err := ui.String(context.Background(), &Nav{ID: 7})
	fmt.Println(out, err)
	fmt.Println((&Nav{ID: 7}).Next(context.Background()))
}
