package main

import (
	"context"
	"fmt"
	"time"

	ui "vukatest.local/stateui"
)

func main() {
	r := &Reminder{}
	fmt.Println(r.hint())
	out, err := ui.String(context.Background(), Stamp{At: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)})
	fmt.Println(out, err)
}
