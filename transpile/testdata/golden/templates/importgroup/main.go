package main

import (
	"context"
	"fmt"

	"github.com/vuka-lang/ui/v2"
)

func main() {
	out, err := ui.String(context.Background(), Total{Days: []int{3, 4}})
	fmt.Println(out, err)
}
