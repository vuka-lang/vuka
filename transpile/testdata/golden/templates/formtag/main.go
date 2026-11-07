package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
)

type Address struct{ City string }

type Signup struct {
	ui.FormState
	Email   string
	Address Address
}

func main() {
	out, err := ui.String(context.Background(), (&Join{}).Render())
	fmt.Println(out, err)
}
