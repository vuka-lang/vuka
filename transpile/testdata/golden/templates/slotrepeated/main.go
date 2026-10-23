package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

func main() {
	out, err := ui.String(context.Background(), Shelf{Books: []Book{{"Go", 380}}})
	fmt.Println(out, err)
}
