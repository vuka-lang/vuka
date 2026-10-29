package main

import (
	"context"
	"fmt"

	ui "vukatest.local/stateui"
)

type ResetLink struct{ Email, Problem string }

// Store counts its calls.
type Store struct {
	calls   *int
	problem string
}

func (s Store) Link(ctx context.Context) (ResetLink, error) {
	*s.calls++
	return ResetLink{Email: "ada@example.com", Problem: s.problem}, nil
}

func main() {
	for _, problem := range []string{"", "expired"} {
		calls := 0
		out, err := ui.String(context.Background(), Page{Store: Store{calls: &calls, problem: problem}})
		fmt.Println(out, err)
		fmt.Println("calls:", calls)
	}
}
